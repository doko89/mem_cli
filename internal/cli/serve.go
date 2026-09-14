package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"mem_cli/internal/transport"
)

func serveCommand(options *options) *cobra.Command {
	var (
		httpAddr  string
		tokenFile string
		rateLimit int64
		maxBody   int64
	)

	command := &cobra.Command{
		Use:   "serve",
		Short: "Run the MCP server (stdio by default, --http for Streamable HTTP)",
		RunE: func(command *cobra.Command, args []string) error {
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			opts := transport.Options{
				DatabasePath: options.databasePath,
				HTTPAddr:     httpAddr,
				TokenFile:    tokenFile,
				RateLimit:    int(rateLimit),
				MaxBodyBytes: maxBody,
			}
			if httpAddr == "" {
				fmt.Fprintln(command.ErrOrStderr(), "mem MCP server listening on stdio")
				return transport.Serve(ctx, opts)
			}
			fmt.Fprintf(command.ErrOrStderr(), "mem MCP server listening on http://%s/mcp\n", httpAddr)
			return transport.Serve(ctx, opts)
		},
	}
	command.Flags().StringVar(&httpAddr, "http", "", "serve Streamable HTTP on this address instead of stdio (e.g. 127.0.0.1:8090)")
	command.Flags().StringVar(&tokenFile, "token-file", "", "path to the HTTP bearer token (default ~/.mem_cli/http_token)")
	command.Flags().Int64Var(&rateLimit, "rate-limit", 60, "HTTP requests per minute per client IP")
	command.Flags().Int64Var(&maxBody, "max-body", 2<<20, "maximum HTTP request body size in bytes")
	return command
}
