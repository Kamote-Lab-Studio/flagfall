package api

import (
	"context"
	"time"
)

// PublishDueForTest runs one scheduler tick.
func (a *API) PublishDueForTest(since time.Time) { a.publishDue(context.Background(), since) }

// PublishScheduledThemes runs the scheduler's theme step.
func (a *API) PublishScheduledThemes(ctx context.Context) { a.publishScheduledThemes(ctx) }
