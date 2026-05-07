package alerts

import "strings"

// unitLookup maps "<kind>/<measurement>" to the SenML unit string.
// An empty string means the measurement is unitless — omit the field from responses.
var unitLookup = map[string]string{
	"ds18b20/temperature": "Cel",
	"ph/ph":               "",
}

// UnitFor resolves the SenML unit for a peripheral string of the form
// "<kind>-<pin>/<measurement>". Returns ("", false) when not found.
func UnitFor(peripheral string) (string, bool) {
	slash := strings.IndexByte(peripheral, '/')
	if slash < 0 {
		return "", false
	}
	measurement := peripheral[slash+1:]
	kindPin := peripheral[:slash]

	dash := strings.LastIndexByte(kindPin, '-')
	var kind string
	if dash < 0 {
		kind = kindPin
	} else {
		kind = kindPin[:dash]
	}

	key := kind + "/" + measurement
	unit, ok := unitLookup[key]
	return unit, ok
}
