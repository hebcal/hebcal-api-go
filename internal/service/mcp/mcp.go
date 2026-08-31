// Package mcp is the Model Context Protocol server behind www.hebcal.com/mcp,
// ported from the Node.js @hebcal/mcp (../hebcal-mcp) so it becomes another
// route on this one binary. It exposes seven calendar tools over a stateless
// streamable-HTTP transport.
//
// All seven tools compute in-process with the same libraries the JSON APIs
// use. The one exception is torah-portion's reading name and summary, which
// come from the readings-svc sidecar's /shabbatTorahReading route because they
// are @hebcal/leyning output (makeSummaryFromParts, and the chag reading label)
// hebcal-go has no counterpart for. That one tool therefore soft-depends on the
// sidecar: with none configured, or an error, the "Reading:" line is omitted
// and the chag portion name falls back to hebcal-go's coarser label, rather
// than the whole tool failing.
//
// The package is named mcp; the SDK it wraps is aliased mcpsdk to keep the two
// apart. The handler layer only calls Handler, so the SDK does not leak past
// this package.
package mcp

import (
	"net/http"
	"strings"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/hebcal/hebcal-api-go/internal/repository/readings"
)

// serverVersion mirrors the version the Node McpServer advertises.
const serverVersion = "1.0.1"

// tools holds the dependencies the tool handlers share. Only torah-portion
// uses rd, and it tolerates a nil client.
type tools struct {
	rd *readings.Client
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
	}, t.convertGregorianToHebrew)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "convert-hebrew-to-gregorian",
		Description: "Converts a Hebrew date to a Gregorian (civil) date",
	}, t.convertHebrewToGregorian)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "yahrzeit",
		Description: "Calculates the Yahrzeit, the anniversary of the day of death of a loved one, according to the Hebrew calendar for a specified date",
	}, t.yahrzeit)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "torah-portion",
		Description: "Calculates the weekly Torah portion (also called parashat haShavua) for a specified date",
	}, t.torahPortion)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "jewish-holidays-year",
		Description: "Calculates a list of all Jewish holidays during a Gregorian (civil) year",
	}, t.jewishHolidaysYear)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "daf-yomi",
		Description: "Calculates the Daf Yomi (Babylonian Talmud) learning for a specified date",
	}, t.dafYomi)

	mcpsdk.AddTool(srv, &mcpsdk.Tool{
		Name:        "shabbat-times",
		Description: "Generates Shabbat and holiday candle-lighting and Havdalah times for a given location and date range",
	}, t.shabbatTimes)

	return srv
}

// Handler returns the stateless streamable-HTTP handler for POST /mcp. In
// stateless mode the SDK answers GET and DELETE with 405, matching the Node
// server. One server instance is shared across requests, as it holds no
// per-session state.
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

// errorCard is the Node errorCard: an ordinary (non-IsError) text result
// carrying a human-readable message, so the model sees the problem and can
// self-correct rather than getting a protocol error.
func errorCard(message string) *mcpsdk.CallToolResult {
	return textResult(message)
}

// lines joins tool output lines the way the Node tools do (results.join('\n')).
func lines(l ...string) *mcpsdk.CallToolResult {
	return textResult(strings.Join(l, "\n"))
}
