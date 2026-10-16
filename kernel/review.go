package kernel

import "github.com/hydrogencms/hydrogen/review"

// wireReview builds the review service and plugs it into the content service.
func (s *Services) wireReview() error {
	r, err := review.New(review.Options{DB: s.DB, Content: s.Content, Access: s.Access, Outbox: s.Outbox,
		Audit: s.Audit, Clock: s.Clock, IDs: s.IDs, Log: s.Log, FourEyes: s.Core.Content.ReviewFourEyes})
	if err != nil {
		return err
	}
	s.Review = r
	s.Content.SetReview(r)
	return nil
}
