package engine

import "context"

type fixtureSource struct {
	events []string
}

func NewFixtureSource(events []string) EventSource {
	return &fixtureSource{events: events}
}

func (f *fixtureSource) Run(ctx context.Context, process func(string) bool) error {
	for _, evt := range f.events {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if process(evt) {
			break
		}
	}
	return nil
}
