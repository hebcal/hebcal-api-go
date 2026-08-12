package geodb

// ZipcodesTzMap maps the numeric timezone column of the ZIP database to an IANA
// tz identifier, matching @hebcal/core Location.ZIPCODES_TZ_MAP.
var ZipcodesTzMap = map[int]string{
	0:  "UTC",
	4:  "America/Puerto_Rico",
	5:  "America/New_York",
	6:  "America/Chicago",
	7:  "America/Denver",
	8:  "America/Los_Angeles",
	9:  "America/Anchorage",
	10: "Pacific/Honolulu",
	11: "Pacific/Pago_Pago",
	13: "Pacific/Funafuti",
	14: "Pacific/Guam",
	15: "Pacific/Palau",
	16: "Pacific/Chuuk",
}

// UsaTzid resolves a US state + numeric timezone + DST flag to an IANA tz
// identifier, matching @hebcal/core Location.getUsaTzid.
func UsaTzid(state string, tz int, dst string) string {
	if tz == 10 && state == "AK" {
		return "America/Adak"
	}
	if tz == 7 && state == "AZ" {
		if dst == "Y" {
			return "America/Denver"
		}
		return "America/Phoenix"
	}
	return ZipcodesTzMap[tz]
}
