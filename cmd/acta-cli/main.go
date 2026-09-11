package main

import (
	"acta/internal/cli"
	"acta/internal/client"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cmd, err := cli.NewCommand(os.Stdin, os.Stdout, os.Stderr)
	if err == nil {
		err = cmd.ExecuteContext(ctx)
	}
	if err != nil {
		structured := false
		if cmd != nil {
			structured, _ = cmd.PersistentFlags().GetBool("json")
		}
		if structured {
			var apiError *client.Error
			var problem any = map[string]string{"code": "command_error", "message": err.Error()}
			if errors.As(err, &apiError) {
				problem = apiError
			}
			_ = json.NewEncoder(os.Stderr).Encode(map[string]any{"error": problem})
		} else {
			fmt.Fprintln(os.Stderr, "Error:", err)
			var apiError *client.Error
			if errors.As(err, &apiError) && len(apiError.Current) > 0 {
				fmt.Fprintln(os.Stderr, "Current field:", string(apiError.Current))
			}
		}
		os.Exit(1)
	}
}
