package kernel

import (
	"context"

	"github.com/hydrogencms/hydrogen/jobs"
	"github.com/hydrogencms/hydrogen/release"
)

// ReleasesJob is the name of the job that publishes due releases.
const ReleasesJob = "releases"

// newReleases builds the release service on the core services.
func newReleases(s *Services) (*release.Service, error) {
	return release.New(release.Options{DB: s.DB, Content: s.Content, Access: s.Access, Auth: s.Auth,
		Outbox: s.Outbox, Audit: s.Audit, Clock: s.Clock, IDs: s.IDs, Log: s.Log})
}

// releaseJob publishes scheduled releases that are due, every minute (the runner wakes each second, so a due
// release starts within about a minute).
func releaseJob(s *Services) jobs.Job {
	return jobs.Job{Name: ReleasesJob, Schedule: "* * * * *", Run: func(ctx context.Context) error {
		_, err := s.Releases.RunDue(ctx)
		return err
	}}
}
