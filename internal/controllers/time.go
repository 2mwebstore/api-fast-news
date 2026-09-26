package controllers

import "time"

// timeNow is a single seam for the current time, so tests can pin it.
var timeNow = func() time.Time { return time.Now().UTC() }
