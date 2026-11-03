package api

import (
	"context"
	"time"
)

// PublishDueForTest runs one scheduler tick.
func (a *API) PublishDueForTest(since time.Time) { a.publishDue(context.Background(), since) }
