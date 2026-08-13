// Package downloadpb is the Download protobuf message carried, base64url
// encoded, in the path of a download.hebcal.com/v4/<data>/<name>.pdf URL.
//
// The schema is shared with hebcal-web, which produces these URLs from the
// hebcal.com download form; download.proto here is a copy of the one it
// serializes against, so the two must be changed together. The message is the
// wire format of a calendar request -- year or month range, location, event
// categories, locale, candle-lighting and Havdalah preferences, daily-learning
// series -- and internal/service/pdf turns it into hebcal.CalOptions.
//
// Regenerate download.pb.go after editing download.proto:
//
//	protoc --go_out=. --go_opt=paths=source_relative \
//	  --proto_path=pkg/downloadpb pkg/downloadpb/download.proto
package downloadpb
