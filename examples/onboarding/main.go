package main

import "github.com/happytoolin/specout/internal/examplekit"

func main() {
	d, api := New()
	examplekit.Run(":8080", "onboarding", d, Handler(api))
}
