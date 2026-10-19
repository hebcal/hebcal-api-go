// Package download turns a download.hebcal.com calendar request into the
// events that calendar holds, whatever format it is served in.
//
// A request arrives as one of three URL shapes naming the same Download
// protobuf (Decode); ParamsFromMessage resolves it into Params -- the
// hebcal.CalOptions plus the presentation choices the formats share -- and
// GenerateCalEvents produces the hebcal-go events. Daily-learning series with
// no Go schedule are listed by UnsupportedSeries and fetched from readings-svc
// by a LearningFetcher. The pdf and csv packages each take it from there.
package download
