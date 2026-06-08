// Behaviour that the generated tests cannot reach.
//
// The generated suites are recorded from Go Example functions, and a call on a
// handle has no literal form -- the object lives in Go -- so none of what makes
// a class a class is covered there. This script covers it, and deliberately
// aims at the four things most likely to be wrong.
//
//   node verify.mjs        (after `gowasm build`)
import assert from "node:assert/strict";
import { test } from "node:test";

const pkg = await import("./node/dist/index.node.js");
const { createClient, GoError, ping } = pkg;

test("a handle is a live object, not a snapshot", async () => {
  const c = await createClient();
  try {
    const s = await c.open("cart", 8);
    assert.equal(await s.name, "cart");

    // The point of a getter: read, mutate through a method, read again.
    assert.equal(await s.writes, 0);
    await s.set("sku", "1234");
    assert.equal(await s.writes, 1, "a stored copy would still read 0 here");

    // And a setter writes back into the same object.
    await s.setName("basket");
    assert.equal(await s.name, "basket");
    assert.equal(await s.label(), "basket (1/8)");
  } finally {
    await c.dispose();
  }
});

// Every call body runs on its own goroutine, so a method issued before close()
// can still be in flight when close() lands.
//
// This asserts the behaviour, not the mechanism. Removing the refcount from the
// Go handle table does not make it fail: the js/wasm scheduler has no
// preemption and runs queued goroutines in order, so a call issued first also
// borrows first. What pins the mechanism is
// TestReleaseDuringBorrowDefersTheDelete in the runtime package, which does
// fail without it. This test is here to notice if that ordering ever stops
// holding.
test("calls issued before close() all settle", async () => {
  const c = await createClient();
  try {
    const s = await c.open("cart", 64);
    await s.set("sku", "1234");

    const inFlight = [];
    for (let i = 0; i < 50; i++) inFlight.push(s.get("sku"));
    const closing = s.close();

    const settled = await Promise.allSettled(inFlight);
    await closing;

    const failed = settled.filter((r) => r.status === "rejected");
    assert.deepEqual(
      failed.map((r) => r.reason?.message),
      [],
      "a call issued before close() must not fail because of it",
    );
    for (const r of settled) assert.equal(r.value, "1234");
  } finally {
    await c.dispose();
  }
});

test("close() is idempotent and a closed handle rejects", async () => {
  const c = await createClient();
  try {
    const s = await c.open("cart", 8);
    await s.close();
    await s.close();

    await assert.rejects(() => s.get("sku"), (e) => e instanceof GoError);
    await assert.rejects(() => s.name, (e) => e instanceof GoError);
    await assert.rejects(() => s.setName("x"), (e) => e instanceof GoError);
  } finally {
    await c.dispose();
  }
});

test("await using releases at the end of the scope", async () => {
  const c = await createClient();
  try {
    let escaped;
    {
      await using s = await c.open("cart", 8);
      escaped = s;
      await s.set("sku", "1234");
      assert.equal(await s.get("sku"), "1234");
    }
    await assert.rejects(() => escaped.get("sku"), (e) => e instanceof GoError);
  } finally {
    await c.dispose();
  }
});

// Every instance numbers handles from 1, so without an owner check a handle
// from one instance would name a real but unrelated object in another.
test("a handle from another instance is refused, not confused", async () => {
  const [a, b] = await Promise.all([createClient(), createClient()]);
  try {
    const mine = await a.open("mine", 8);
    const theirs = await b.open("theirs", 8);
    await theirs.set("k", "v");

    await assert.rejects(
      () => mine.merge(theirs),
      (e) => e instanceof GoError && /different instance/.test(e.message),
    );
    // And the target was not touched.
    assert.deepEqual(await mine.keys(), []);
  } finally {
    await Promise.all([a.dispose(), b.dispose()]);
  }
});

// The shared instance behind the named exports is never disposed, so a handle
// nobody closes is held for the life of the process. __stats is what makes that
// observable rather than mysterious.
test("handles are released, and the count proves it", async () => {
  const c = await createClient();
  try {
    const before = await c.liveHandles();

    const many = [];
    for (let i = 0; i < 500; i++) many.push(await c.open(`s${i}`, 2));
    const peak = await c.liveHandles();
    assert.equal(peak - before, 500);

    for (const s of many) await s.close();
    const after = await c.liveHandles();
    assert.equal(after, before, "every handle should have been freed");
  } finally {
    await c.dispose();
  }
});

test("classes compose: a method returns another class, and back", async () => {
  const c = await createClient();
  try {
    const s = await c.open("cart", 8);
    const t = await s.txn();
    await t.set("a", "1");
    await t.set("b", "2");
    assert.equal(await t.commit(), 2);
    assert.deepEqual(await s.keys(), ["a", "b"]);

    // Txn.store() hands the same Go object back, though not the same wrapper.
    const same = await t.store();
    assert.notEqual(same, s, "two handles to one object are two JS objects");
    assert.equal(await same.name, "cart");

    // A nil *Store arrives as null, and a real one as a class.
    assert.equal(await s.parent(), null);
    const child = await s.child("wishlist");
    assert.equal(await (await child.parent()).name, "cart");
  } finally {
    await c.dispose();
  }
});

test("methods carry the ordinary rules: errors, bytes, variadic, context", async () => {
  const c = await createClient();
  try {
    const s = await c.open("cart", 2);
    await s.set("a", "1");

    await assert.rejects(
      () => s.get("missing"),
      (e) => e instanceof GoError && e.message === 'no key "missing" in cart',
    );

    const blob = await s.blob("a");
    assert.ok(blob instanceof Uint8Array, "[]byte from a method is a Uint8Array");
    assert.equal(new TextDecoder().decode(blob), "1");

    assert.deepEqual(await s.tags("a", "zz"), ["a"]);
    assert.deepEqual(await s.tags(), []);
    assert.equal(await s.count(), 1, "the context parameter is not in the signature");

    // A method returning a data struct still crosses as plain JSON.
    assert.deepEqual(await s.snapshot(), { name: "cart", keys: ["a"], count: 1 });

    await s.set("b", "2");
    await assert.rejects(() => s.set("c", "3"), (e) => e instanceof GoError);
  } finally {
    await c.dispose();
  }
});

test("free functions still work alongside classes", async () => {
  assert.equal(await ping(), "session");
  await pkg.dispose();
});

test("a method after the instance is disposed rejects rather than hanging", async () => {
  const c = await createClient();
  const s = await c.open("cart", 8);
  await c.dispose();
  await assert.rejects(() => s.get("sku"), (e) => e instanceof GoError);
});
