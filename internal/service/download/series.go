package download

import (
	"fmt"
	"slices"

	"github.com/hebcal/hebcal-go/dailylearning"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/hebcal/hebcal-api-go/internal/webconfig"
	"github.com/hebcal/hebcal-api-go/pkg/downloadpb"
)

// series is one dailyLearningConfig.json entry resolved against this build:
// its Download protobuf field, and whether hebcal-go can generate it.
type series struct {
	webconfig.LearningSeries
	field protoreflect.FieldDescriptor
	// generated: the series' schedule is in the dailylearning registry. A
	// daily-learning series that is not comes from readings-svc instead.
	generated bool
}

// learning reports whether the entry is a daily-learning series, rather than
// the Torah readings the file also lists.
func (s *series) learning() bool { return s.DailyLearningOptName != "" }

// on reports whether msg asks for the series.
func (s *series) on(msg *downloadpb.Download) bool {
	return msg.ProtoReflect().Get(s.field).Bool()
}

var (
	// allSeries is every entry, in the file's order.
	allSeries = resolveSeries(webconfig.Learning())
	// learningSchedules are the series hebcal-go generates, in day order.
	learningSchedules = filterSeries(inDayOrder(allSeries), func(s *series) bool { return s.generated })
	// fallbackSeries are the daily-learning series readings-svc supplies.
	fallbackSeries = filterSeries(allSeries, func(s *series) bool { return s.learning() && !s.generated })
	// learningOrder is every daily-learning series' EventCategory, in day
	// order.
	learningOrder = categories(filterSeries(inDayOrder(allSeries), (*series).learning))
)

// resolveSeries looks up each entry's protobuf field and schedule. The file is
// embedded, so an entry this build cannot use is a programming error, caught by
// any test that loads the package.
func resolveSeries(cfg []webconfig.LearningSeries) []series {
	fields := (&downloadpb.Download{}).ProtoReflect().Descriptor().Fields()
	out := make([]series, len(cfg))
	for i, c := range cfg {
		fd := fields.ByName(protoreflect.Name(c.ProtocName))
		if fd == nil || fd.Kind() != protoreflect.BoolKind || fd.IsList() {
			panic(fmt.Sprintf("dailyLearningConfig.json: %q: protocName %q is not a bool field of "+
				"the Download protobuf (regenerate pkg/downloadpb from hebcal-web's download.proto)",
				c.EventCategory, c.ProtocName))
		}
		out[i] = series{
			LearningSeries: c,
			field:          fd,
			generated:      c.DailyLearningOptName != "" && dailylearning.Has(c.DailyLearningOptName),
		}
	}
	return out
}

// inDayOrder is the order a day's learning rows appear in: the file's, except
// that a series whose schedule name is integer-like (929) comes first. The
// website keeps the selected series in a JavaScript object, whose
// integer-like keys iterate before all others.
func inDayOrder(in []series) []series {
	out := slices.Clone(in)
	slices.SortStableFunc(out, func(a, b series) int {
		ai, bi := integerLike(a.DailyLearningOptName), integerLike(b.DailyLearningOptName)
		switch {
		case ai && !bi:
			return -1
		case bi && !ai:
			return 1
		}
		return 0
	})
	return out
}

// integerLike reports whether s is a canonical array index in JavaScript's
// sense: digits without a leading zero.
func integerLike(s string) bool {
	if s == "" || (len(s) > 1 && s[0] == '0') {
		return false
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func filterSeries(in []series, keep func(*series) bool) []series {
	var out []series
	for i := range in {
		if keep(&in[i]) {
			out = append(out, in[i])
		}
	}
	return out
}

func categories(in []series) []string {
	out := make([]string, len(in))
	for i := range in {
		out[i] = in[i].EventCategory
	}
	return out
}
