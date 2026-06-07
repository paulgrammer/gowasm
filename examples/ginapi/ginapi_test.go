package ginapi_test

import (
	"fmt"

	"example.com/ginapi"
)

func ExampleStart() {
	info, _ := ginapi.Start(ginapi.Config{BasePath: "/api"})
	fmt.Println(info.Mode, info.BasePath)
	// Output: release /api
}

func ExampleHandle() {
	ginapi.Start(ginapi.Config{})
	res, _ := ginapi.Handle(ginapi.Request{Method: "GET", Path: "/search?q=gowasm"})
	fmt.Println(res.Status, res.Body)
	// Output: 200 {"matches":[],"query":"gowasm"}
}

func ExampleHandle_validationFailure() {
	ginapi.Start(ginapi.Config{})
	res, _ := ginapi.Handle(ginapi.Request{
		Method: "POST",
		Path:   "/users",
		Body:   `{"name":"Ada"}`,
	})
	fmt.Println(res.Status)
	// Output: 400
}

func ExampleHandle_notFound() {
	ginapi.Start(ginapi.Config{})
	res, _ := ginapi.Handle(ginapi.Request{Method: "GET", Path: "/nope"})
	fmt.Println(res.Status)
	// Output: 404
}

func ExampleHandle_panicRecovered() {
	ginapi.Start(ginapi.Config{})
	res, _ := ginapi.Handle(ginapi.Request{Method: "GET", Path: "/boom"})
	fmt.Println(res.Status)
	// Output: 500
}

func ExampleAddRoute() {
	ginapi.Start(ginapi.Config{})
	route, _ := ginapi.AddRoute("GET", "/stub", 200, `{"stub":true}`, "")
	fmt.Println(route.Method, route.Path, route.Source)
	// Output: GET /stub runtime
}

func ExampleAddRoute_badStatus() {
	ginapi.Start(ginapi.Config{})
	_, err := ginapi.AddRoute("GET", "/stub", 99, "", "")
	fmt.Println(err)
	// Output: status must be between 100 and 599, got 99
}

func ExampleRemoveRoute() {
	ginapi.Start(ginapi.Config{})
	err := ginapi.RemoveRoute("GET", "/never-added")
	fmt.Println(err)
	// Output: no runtime route for GET /never-added
}
