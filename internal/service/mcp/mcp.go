// Package mcp is the Model Context Protocol server behind www.hebcal.com/mcp.
// It exposes seven calendar tools over a stateless streamable-HTTP transport.
//
// All seven tools compute in-process with the same libraries the JSON APIs
// use. The one exception is torah-portion's reading name and summary, which
// come from the readings-svc sidecar's /shabbatTorahReading route because the
// merged verse-range summary and the chag reading label have no hebcal-go
// counterpart. That one tool therefore soft-depends on the sidecar: with none
// configured, or an error, the "Reading:" line is omitted and the chag portion
// name falls back to hebcal-go's coarser label, rather than the whole tool
// failing.
//
// The package is named mcp; the SDK it wraps is aliased mcpsdk to keep the two
// apart. The handler layer only calls Handler, so the SDK does not leak past
// this package.
package mcp

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"runtime/debug"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hebcal/hebcal-api-go/internal/config"
	"github.com/hebcal/hebcal-api-go/internal/repository/readings"
)

// serverVersion is the application's own build version, without the
// leading "v" the module version carries.
var serverVersion = strings.TrimPrefix(config.APIVersion, "v")

// tools holds the dependencies the tool handlers share. Only torah-portion
// uses rd, and it tolerates a nil client.
type tools struct {
	rd *readings.Client
}

// guard wraps a tool handler so a panic becomes a returned error instead of
// crashing the whole process. The MCP SDK runs each tool in its own goroutine
// (jsonrpc2's handleAsync), outside net/http's per-request recover, so an
// unguarded panic in one tool takes the entire binary down -- a request-driven
// crash for every route, not just /mcp. The stack is written to stderr so the
// panic is still visible in journald.
func guard[In any](name string, h mcpsdk.ToolHandlerFor[In, any]) mcpsdk.ToolHandlerFor[In, any] {
	return func(ctx context.Context, req *mcpsdk.CallToolRequest, in In) (res *mcpsdk.CallToolResult, out any, err error) {
		defer func() {
			if r := recover(); r != nil {
				fmt.Fprintf(os.Stderr, "mcp: recovered panic in tool %s: %v\n%s", name, r, debug.Stack())
				res, out, err = errorCard("Internal error computing "+name), nil, nil
			}
		}()
		return h(ctx, req, in)
	}
}

// NewServer builds the MCP server with all seven hebcal tools registered. rd
// may be nil, in which case torah-portion omits the readings-svc "Reading:"
// line.
func NewServer(rd *readings.Client) *mcpsdk.Server {
	t := &tools{rd: rd}
	srv := mcpsdk.NewServer(
		&mcpsdk.Implementation{Name: "hebcal", Version: serverVersion},
		&mcpsdk.ServerOptions{
			// hebcal's tool set is static; advertising listChanged makes clients
			// hold a subscriptions/listen stream open (SEP-2575) that Varnish's
			// 10s first_byte_timeout then kills every 10s. Pin it off.
			Capabilities: &mcpsdk.ServerCapabilities{
				Tools: &mcpsdk.ToolCapabilities{}, // ListChanged: false
			},
		},
	)
	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "convert-gregorian-to-hebrew",
		Description: "Converts a Gregorian (civil) date to a Hebrew date (Jewish calendar)",
	}, guard("convert-gregorian-to-hebrew", t.convertGregorianToHebrew))

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "convert-hebrew-to-gregorian",
		Description: "Converts a Hebrew date to a Gregorian (civil) date",
	}, guard("convert-hebrew-to-gregorian", t.convertHebrewToGregorian))

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "yahrzeit",
		Description: "Calculates the Yahrzeit, the anniversary of the day of death of a loved one, according to the Hebrew calendar for a specified date",
	}, guard("yahrzeit", t.yahrzeit))

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "torah-portion",
		Description: "Calculates the weekly Torah portion (also called parashat haShavua) for a specified date",
	}, guard("torah-portion", t.torahPortion))

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "jewish-holidays-year",
		Description: "Calculates a list of all Jewish holidays during a Gregorian (civil) year",
	}, guard("jewish-holidays-year", t.jewishHolidaysYear))

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "daf-yomi",
		Description: "Calculates the Daf Yomi (Babylonian Talmud) learning for a specified date",
	}, guard("daf-yomi", t.dafYomi))

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "shabbat-times",
		Description: "Generates Shabbat and holiday candle-lighting and Havdalah times for a given location and date range",
	}, guard("shabbat-times", t.shabbatTimes))

	return srv
}

// Handler returns the stateless streamable-HTTP handler for POST /mcp. In
// stateless mode the SDK answers GET and DELETE with 405. One server instance
// is shared across requests, as it holds no per-session state.
func Handler(rd *readings.Client) http.Handler {
	srv := NewServer(rd)
	return mcpsdk.NewStreamableHTTPHandler(
		func(*http.Request) *mcpsdk.Server { return srv },
		&mcpsdk.StreamableHTTPOptions{Stateless: true},
	)
}

// textResult wraps a plain-text tool result, the shape every hebcal tool
// returns.
func textResult(s string) *mcpsdk.CallToolResult {
	return &mcpsdk.CallToolResult{
		Content: []mcpsdk.Content{&mcpsdk.TextContent{Text: s}},
	}
}

// errorCard returns an ordinary (non-IsError) text result carrying a
// human-readable message, so the model sees the problem and can self-correct
// rather than getting a protocol error.
func errorCard(message string) *mcpsdk.CallToolResult {
	return textResult(message)
}

// lines joins tool output lines with newlines.
func lines(l ...string) *mcpsdk.CallToolResult {
	return textResult(strings.Join(l, "\n"))
}
