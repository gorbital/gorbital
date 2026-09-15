package app_test

import (
	"context"
	"fmt"
	"time"

	"gorbital.dev/app"
)

func ExampleRun() {
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(10*time.Millisecond, cancel) // stands in for SIGTERM

	cleanup := &app.Cleanup{}
	cleanup.Add("database", func(context.Context) error {
		fmt.Println("database closed")
		return nil
	})

	worker := app.RunnerFunc(func(ctx context.Context) error {
		fmt.Println("worker started")
		<-ctx.Done()
		fmt.Println("worker stopped")
		return nil
	})

	err := app.Run(ctx, []app.Runner{worker},
		app.WithSignals(),
		app.WithDrainDelay(0),
		app.WithCleanup(cleanup),
	)
	fmt.Println("error:", err)
	// Output:
	// worker started
	// worker stopped
	// database closed
	// error: <nil>
}
