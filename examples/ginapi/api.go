// Package ginapi runs a Gin application inside WebAssembly and lets JavaScript
// drive it.
//
// # Why there is no ListenAndServe here
//
// Under GOOS=js GOARCH=wasm the net package is backed by a fake, in-process
// network stack. net.Listen appears to succeed, but nothing outside the
// WebAssembly instance can connect to it, so a Gin server started the usual way
// would be unreachable. Trying anyway is the main way people get stuck.
//
// What does work is the layer underneath: an *gin.Engine is an http.Handler, and
// serving one request is just ServeHTTP. This package exposes that as Handle,
// so JavaScript passes a request in and gets a response back. Put Node's own
// http server in front of it — see serve.mjs — and you have a real HTTP server
// on a real port whose routing, binding, validation and middleware all run in Go.
//
// Routes come from two places: those declared in Go below, and those registered
// at runtime from JavaScript with AddRoute. Registering Go *handlers* from
// JavaScript is deliberately not offered: a handler is code, and only data
// crosses this boundary.
package ginapi

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// Config controls how the engine is built.
type Config struct {
	// Debug turns on Gin's development logging. Output goes to the console.
	Debug bool `json:"debug,omitempty"`
	// BasePath prefixes every route, for example "/api/v1".
	BasePath string `json:"basePath,omitempty"`
}

// Info describes a started engine.
type Info struct {
	Mode     string  `json:"mode"`
	BasePath string  `json:"basePath"`
	Routes   []Route `json:"routes"`
}

// Route is one registered endpoint.
type Route struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	// Source is "go" for routes declared in this package, or "runtime" for
	// routes added from JavaScript.
	Source string `json:"source"`
}

// Request is an incoming HTTP request.
type Request struct {
	Method string `json:"method"`
	// Path may include a query string, for example "/search?q=go".
	Path    string            `json:"path"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body,omitempty"`
}

// Response is what the engine produced.
type Response struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    string            `json:"body"`
}

// User is the resource the built-in routes manage.
type User struct {
	ID    int    `json:"id"`
	Name  string `json:"name" binding:"required"`
	Email string `json:"email" binding:"required,email"`
}

// Status reports what the engine has handled.
type Status struct {
	Started  bool `json:"started"`
	Requests int  `json:"requests"`
	Users    int  `json:"users"`
	Dynamic  int  `json:"dynamicRoutes"`
}

// server holds everything the engine needs. Every exported function runs on its
// own goroutine, so all shared state is guarded.
type server struct {
	mu       sync.RWMutex
	engine   *gin.Engine
	basePath string
	mode     string
	requests int

	users  map[int]User
	nextID int

	// dynamic holds routes registered from JavaScript. Gin's router cannot
	// remove a route once added, so these are served through NoRoute instead of
	// being registered with it, which makes AddRoute and RemoveRoute symmetric.
	dynamic map[string]dynamicRoute
}

type dynamicRoute struct {
	status      int
	body        string
	contentType string
}

var srv = &server{}

// Start builds the engine and registers the built-in routes. Calling it again
// rebuilds from scratch, discarding stored users and dynamic routes.
func Start(cfg Config) (Info, error) {
	srv.mu.Lock()
	defer srv.mu.Unlock()

	mode := gin.ReleaseMode
	if cfg.Debug {
		mode = gin.DebugMode
	}
	gin.SetMode(mode)

	base := cfg.BasePath
	if base != "" && !strings.HasPrefix(base, "/") {
		return Info{}, fmt.Errorf("basePath must start with /, got %q", base)
	}

	srv.engine = gin.New()
	// Recovery turns a panic in a handler into a 500 rather than tearing down
	// the whole WebAssembly instance, which a panic would otherwise do.
	srv.engine.Use(gin.Recovery())
	if cfg.Debug {
		srv.engine.Use(gin.Logger())
	}

	srv.basePath = base
	srv.mode = mode
	srv.users = map[int]User{}
	srv.nextID = 1
	srv.dynamic = map[string]dynamicRoute{}
	srv.requests = 0

	group := srv.engine.Group(base)
	registerRoutes(group)

	srv.engine.NoRoute(serveDynamic)

	return Info{Mode: mode, BasePath: base, Routes: srv.routesLocked()}, nil
}

func registerRoutes(r *gin.RouterGroup) {
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok", "time": time.Now().UTC().Format(time.RFC3339)})
	})

	r.GET("/users", func(c *gin.Context) {
		srv.mu.RLock()
		defer srv.mu.RUnlock()

		out := make([]User, 0, len(srv.users))
		for _, u := range srv.users {
			out = append(out, u)
		}
		sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
		c.JSON(http.StatusOK, out)
	})

	// A path parameter, which Gin binds for us.
	r.GET("/users/:id", func(c *gin.Context) {
		var id int
		if _, err := fmt.Sscanf(c.Param("id"), "%d", &id); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "id must be a number"})
			return
		}
		srv.mu.RLock()
		u, ok := srv.users[id]
		srv.mu.RUnlock()
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("no user %d", id)})
			return
		}
		c.JSON(http.StatusOK, u)
	})

	// Gin's binding tags do the validation; a bad body never reaches our logic.
	r.POST("/users", func(c *gin.Context) {
		var in User
		if err := c.ShouldBindJSON(&in); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		srv.mu.Lock()
		in.ID = srv.nextID
		srv.nextID++
		srv.users[in.ID] = in
		srv.mu.Unlock()

		c.JSON(http.StatusCreated, in)
	})

	r.DELETE("/users/:id", func(c *gin.Context) {
		var id int
		if _, err := fmt.Sscanf(c.Param("id"), "%d", &id); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "id must be a number"})
			return
		}
		srv.mu.Lock()
		_, ok := srv.users[id]
		delete(srv.users, id)
		srv.mu.Unlock()
		if !ok {
			c.JSON(http.StatusNotFound, gin.H{"error": fmt.Sprintf("no user %d", id)})
			return
		}
		c.Status(http.StatusNoContent)
	})

	// A query parameter, plus a deliberate panic route to show that Recovery
	// keeps the instance alive.
	r.GET("/search", func(c *gin.Context) {
		q := c.Query("q")
		if q == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "q is required"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"query": q, "matches": []string{}})
	})

	r.GET("/boom", func(c *gin.Context) {
		panic("this handler panics on purpose")
	})
}

// serveDynamic answers routes registered from JavaScript, and 404s otherwise.
func serveDynamic(c *gin.Context) {
	srv.mu.RLock()
	route, ok := srv.dynamic[routeKey(c.Request.Method, c.Request.URL.Path)]
	srv.mu.RUnlock()

	if !ok {
		c.JSON(http.StatusNotFound, gin.H{
			"error": fmt.Sprintf("no route for %s %s", c.Request.Method, c.Request.URL.Path),
		})
		return
	}
	c.Data(route.status, route.contentType, []byte(route.body))
}

// Stop releases the engine. Handle then reports that nothing is running.
func Stop() error {
	srv.mu.Lock()
	defer srv.mu.Unlock()
	if srv.engine == nil {
		return fmt.Errorf("the engine is not running")
	}
	srv.engine = nil
	srv.users = nil
	srv.dynamic = nil
	return nil
}

// AddRoute registers a fixed response from JavaScript, which is useful for
// stubbing an endpoint the Go code does not implement.
func AddRoute(method, path string, status int, body, contentType string) (Route, error) {
	method = strings.ToUpper(strings.TrimSpace(method))
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodHead:
	default:
		return Route{}, fmt.Errorf("unsupported method %q", method)
	}
	if !strings.HasPrefix(path, "/") {
		return Route{}, fmt.Errorf("path must start with /, got %q", path)
	}
	if status < 100 || status > 599 {
		return Route{}, fmt.Errorf("status must be between 100 and 599, got %d", status)
	}
	if contentType == "" {
		contentType = "application/json; charset=utf-8"
	}

	srv.mu.Lock()
	defer srv.mu.Unlock()
	if srv.engine == nil {
		return Route{}, fmt.Errorf("the engine is not running; call start first")
	}
	srv.dynamic[routeKey(method, path)] = dynamicRoute{status: status, body: body, contentType: contentType}
	return Route{Method: method, Path: path, Source: "runtime"}, nil
}

// RemoveRoute drops a route added by AddRoute.
func RemoveRoute(method, path string) error {
	key := routeKey(strings.ToUpper(method), path)

	srv.mu.Lock()
	defer srv.mu.Unlock()
	if srv.engine == nil {
		return fmt.Errorf("the engine is not running; call start first")
	}
	if _, ok := srv.dynamic[key]; !ok {
		return fmt.Errorf("no runtime route for %s %s", strings.ToUpper(method), path)
	}
	delete(srv.dynamic, key)
	return nil
}

// Routes lists every route the engine will answer.
func Routes() ([]Route, error) {
	srv.mu.RLock()
	defer srv.mu.RUnlock()
	if srv.engine == nil {
		return nil, fmt.Errorf("the engine is not running; call start first")
	}
	return srv.routesLocked(), nil
}

func (s *server) routesLocked() []Route {
	out := make([]Route, 0, len(s.dynamic))
	for _, r := range s.engine.Routes() {
		out = append(out, Route{Method: r.Method, Path: r.Path, Source: "go"})
	}
	for key := range s.dynamic {
		method, path, _ := strings.Cut(key, " ")
		out = append(out, Route{Method: method, Path: path, Source: "runtime"})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Method < out[j].Method
	})
	return out
}

// Handle runs one request through the engine and returns what it produced.
//
// This is ServeHTTP with the transport removed: no socket is involved, so it
// works inside WebAssembly where listening does not.
func Handle(req Request) (Response, error) {
	srv.mu.RLock()
	engine := srv.engine
	srv.mu.RUnlock()
	if engine == nil {
		return Response{}, fmt.Errorf("the engine is not running; call start first")
	}

	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method == "" {
		method = http.MethodGet
	}
	path := req.Path
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	httpReq, err := http.NewRequest(method, path, strings.NewReader(req.Body))
	if err != nil {
		return Response{}, fmt.Errorf("building request: %w", err)
	}
	for k, v := range req.Headers {
		httpReq.Header.Set(k, v)
	}
	if req.Body != "" && httpReq.Header.Get("Content-Type") == "" {
		httpReq.Header.Set("Content-Type", "application/json")
	}

	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httpReq)

	srv.mu.Lock()
	srv.requests++
	srv.mu.Unlock()

	headers := make(map[string]string, len(rec.Header()))
	for k := range rec.Header() {
		headers[k] = rec.Header().Get(k)
	}
	return Response{Status: rec.Code, Headers: headers, Body: rec.Body.String()}, nil
}

// Describe reports what the engine has handled so far.
func Describe() Status {
	srv.mu.RLock()
	defer srv.mu.RUnlock()
	return Status{
		Started:  srv.engine != nil,
		Requests: srv.requests,
		Users:    len(srv.users),
		Dynamic:  len(srv.dynamic),
	}
}

func routeKey(method, path string) string { return method + " " + path }
