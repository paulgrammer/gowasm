// Helpers shared by every demo page.
//
// Each demo imports its own generated package; this only handles the parts that
// are the same everywhere: reporting load progress, showing errors in a way
// that distinguishes a Go error from a broken page, and not letting a slow
// module look like a hung one.

/** Loads a generated package, reporting progress into an element. */
export async function load(specifier, statusEl) {
  const started = performance.now();
  const say = (t) => { if (statusEl) statusEl.textContent = t; };

  say("loading WebAssembly module…");
  try {
    const api = await import(specifier);
    // Touching the API boots the instance, so the first real call is not the
    // one that pays for start-up.
    if (typeof api.createClient === "function") await api.createClient();
    say(`ready in ${Math.round(performance.now() - started)} ms`);
    return api;
  } catch (err) {
    say("");
    throw new Error(
      `could not load ${specifier}: ${err?.message ?? err}\n\n` +
      `Run "make demo" from the repository root to build the packages this page needs.`,
    );
  }
}

/** Renders a value into an element, formatting errors distinctly. */
export function show(el, value) {
  if (value instanceof Error) {
    el.className = "out err";
    el.textContent = value.message;
    return;
  }
  el.className = "out";
  el.textContent = typeof value === "string" ? value : JSON.stringify(value, null, 2);
}

/** Wraps a handler so a thrown error lands in the output rather than the console. */
export function wire(el, fn) {
  return async (...args) => {
    try {
      const out = await fn(...args);
      if (out !== undefined) show(el, out);
    } catch (err) {
      show(el, err instanceof Error ? err : new Error(String(err)));
    }
  };
}

/** Re-runs fn shortly after the user stops typing. */
export function onInput(el, fn, ms = 150) {
  let t;
  el.addEventListener("input", () => {
    clearTimeout(t);
    t = setTimeout(fn, ms);
  });
}

/** Offers bytes to the user as a file download. */
export function download(bytes, filename, type) {
  const url = URL.createObjectURL(new Blob([bytes], { type }));
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  a.click();
  URL.revokeObjectURL(url);
}
