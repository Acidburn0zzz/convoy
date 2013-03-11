package common

import "regexp"
import "strconv"

const (
	numRe = `(\d+(?:\.\d+)?)`
)

var degMinSecRe = regexp.MustCompile(
	numRe + `°(?:` + numRe + `′)?(?:` + numRe + `″)?([NSWE])`)

func StringToDegrees(text string) float64 {
	m := degMinSecRe.FindStringSubmatch(text)
	if m == nil {
		return 0
	}
	deg, _ := strconv.ParseFloat(m[1], 64)
	min, _ := strconv.ParseFloat(m[2], 64)
	sec, _ := strconv.ParseFloat(m[3], 64)
	dir := m[4]
	
	angle := deg + (min / 60.0) + (sec / 3600)
	
	if dir == "S" || dir == "W" {
		angle = - angle
	}	

	return angle
}
