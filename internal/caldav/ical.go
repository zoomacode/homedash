package caldav

import (
	"time"

	"github.com/emersion/go-ical"
)

// getProp returns the raw string value of a named property, or "" if absent.
func getProp(c *ical.Component, name string) string {
	if p := c.Props.Get(name); p != nil {
		return p.Value
	}
	return ""
}

// getTime parses a date-time property from a component.
// Returns the zero Time and false when the property is absent or unparseable.
func getTime(c *ical.Component, name string) (time.Time, bool) {
	p := c.Props.Get(name)
	if p == nil {
		return time.Time{}, false
	}
	t, err := p.DateTime(nil)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}
