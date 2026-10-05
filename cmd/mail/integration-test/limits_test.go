package integrationtest

import (
	"fmt"
	"net/http"
	"time"

	"github.com/piggystats/nodera/contexts/mail/async/constants"
)

func (s *mailSuite) TestLimits_DailyLimitDoublesAfterACleanDay() {
	p, key := s.ready()
	s.Require().Equal(http.StatusAccepted, s.sendTo(key, "no-reply@example.com", addresses(50, "a")...).Code)
	s.Require().Equal(http.StatusAccepted, s.sendTo(key, "no-reply@example.com", addresses(50, "b")...).Code)

	s.clock.Set(time.Date(2026, 10, 13, 0, 30, 0, 0, time.UTC))
	s.Require().NoError(s.jobs.EvaluateDays(s.T().Context()))
	s.Equal(float64(400), s.overview(p).obj("daily_limit")["limit"])

	s.Require().NoError(s.jobs.EvaluateDays(s.T().Context()))
	s.Equal(float64(400), s.overview(p).obj("daily_limit")["limit"], "a day is judged once")
}

// A day whose sending all came before the decision about the day before it
// still earns its doubling, on the limit that decision left.
func (s *mailSuite) TestLimits_ADaySentBeforeTheLastDecisionStillDoubles() {
	p, key := s.ready()
	s.sendMany(key, 100)

	s.clock.Set(time.Date(2026, 10, 13, 0, 1, 0, 0, time.UTC))
	s.sendMany(key, 100)
	s.clock.Set(time.Date(2026, 10, 13, 0, 20, 0, 0, time.UTC))
	s.Require().NoError(s.jobs.EvaluateDays(s.T().Context()))
	s.Equal(float64(400), s.overview(p).obj("daily_limit")["limit"])

	s.clock.Set(time.Date(2026, 10, 14, 0, 20, 0, 0, time.UTC))
	s.Require().NoError(s.jobs.EvaluateDays(s.T().Context()))
	s.Equal(float64(800), s.overview(p).obj("daily_limit")["limit"])
}

// A worker that was down catches every missed day up, each on the limit the
// day before it left.
func (s *mailSuite) TestLimits_MissedDaysAreCaughtUpOneByOne() {
	p, key := s.ready()
	for day := 12; day <= 14; day++ {
		s.clock.Set(time.Date(2026, 10, day, 10, 0, 0, 0, time.UTC))
		s.sendMany(key, 100)
	}

	s.clock.Set(time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC))
	s.Require().NoError(s.jobs.EvaluateDays(s.T().Context()))

	s.Equal(float64(1600), s.overview(p).obj("daily_limit")["limit"])
}

// A Message the allowance cannot take whole is refused, but smaller ones
// still fit, so the client is told how many emails are left.
func (s *mailSuite) TestLimits_AMessageLargerThanWhatIsLeftIsRefusedWithWhatIsLeft() {
	p, key := s.ready()
	s.exec(`UPDATE mail_periods SET used = 9995 WHERE project_id = $1`, p.ProjectID)

	refused := s.sendTo(key, "no-reply@example.com", addresses(10, "big-")...)
	s.expectError(refused, http.StatusTooManyRequests, 53, 10005)
	s.Contains(refused.Raw, "5 emails left")
	s.NotEmpty(refused.Hdr.Get("Retry-After"))
	s.Equal("active", s.overview(p).str("sending_state"))

	s.Require().Equal(http.StatusAccepted, s.sendTo(key, "no-reply@example.com", addresses(5, "small-")...).Code)
	s.Equal("exhausted", s.overview(p).str("sending_state"))
	s.Len(s.outboxPayloads(constants.MailPackageExhausted), 1)
}

// The allowance is checked before the Daily limit, as the overview reports
// them: an allowance used up outlasts a day.
func (s *mailSuite) TestLimits_AUsedUpAllowanceIsReportedBeforeTheDailyLimit() {
	p, key := s.ready()
	s.sendMany(key, 200)
	s.exec(`UPDATE mail_periods SET used = allowance WHERE project_id = $1`, p.ProjectID)

	s.expectError(s.sendTo(key, "no-reply@example.com", "ana@example.net"), http.StatusTooManyRequests, 53, 10003)
	s.Equal("exhausted", s.overview(p).str("sending_state"))
}

func (s *mailSuite) TestLimits_LittleUseOrBouncesKeepTheLimit() {
	p, key := s.ready()
	id := s.sendTo(key, "no-reply@example.com", addresses(50, "a")...).str("id")
	s.Require().Equal(http.StatusAccepted, s.sendTo(key, "no-reply@example.com", addresses(49, "b")...).Code)

	s.clock.Set(time.Date(2026, 10, 13, 0, 30, 0, 0, time.UTC))
	s.Require().NoError(s.jobs.EvaluateDays(s.T().Context()))
	s.Equal(float64(200), s.overview(p).obj("daily_limit")["limit"], "99 of 200 is under half")

	s.Require().Equal(http.StatusAccepted, s.sendTo(key, "no-reply@example.com", addresses(50, "c")...).Code)
	s.Require().Equal(http.StatusAccepted, s.sendTo(key, "no-reply@example.com", addresses(50, "d")...).Code)
	for i := range 2 {
		s.post(s.delivery(id, fmt.Sprintf("a%d@example.net", i), "Bounce", "InvalidRecipient", 550, `{"class":5,"subject":1,"detail":1}`))
	}
	s.clock.Set(time.Date(2026, 10, 14, 0, 30, 0, 0, time.UTC))
	s.Require().NoError(s.jobs.EvaluateDays(s.T().Context()))
	s.Equal(float64(200), s.overview(p).obj("daily_limit")["limit"], "2 bounces in 100 is the warning line")
}

// sendMany sends n recipients in Messages of 50 and answers their ids.
func (s *mailSuite) sendMany(key string, n int) []string {
	var ids []string
	for i := 0; n > 0; i++ {
		batch := min(n, 50)
		r := s.sendTo(key, "no-reply@example.com", addresses(batch, fmt.Sprintf("m%d-", i))...)
		s.Require().Equal(http.StatusAccepted, r.Code, r.Raw)
		ids = append(ids, r.str("id"))
		n -= batch
	}

	return ids
}

func (s *mailSuite) bounce(messageID string, batch, i int) {
	s.Require().Equal(http.StatusNoContent, s.post(s.delivery(messageID, fmt.Sprintf("m%d-%d@example.net", batch, i), "Bounce", "InvalidRecipient", 550, `{"class":5,"subject":1,"detail":1}`)).Code)
}

func (s *mailSuite) TestLimits_RatesWarnThenPauseAndAnOperatorLifts() {
	p, key := s.ready()
	s.exec(`UPDATE project_limits SET daily_limit = 100000 WHERE project_id = $1`, p.ProjectID)
	ids := s.sendMany(key, 600)

	for i := range 13 {
		s.bounce(ids[0], 0, i)
	}
	s.Require().NoError(s.jobs.JudgeRates(s.T().Context()))
	warnings := s.outboxPayloads(constants.MailRatesWarning)
	s.Require().Len(warnings, 1)
	s.Equal("2.17", warnings[0]["bounce_rate"])
	s.Equal(float64(7), warnings[0]["window_days"])
	s.Require().NoError(s.jobs.JudgeRates(s.T().Context()))
	s.Len(s.outboxPayloads(constants.MailRatesWarning), 1, "a warning at most once a day")

	for i := 13; i < 30; i++ {
		s.bounce(ids[0], 0, i)
	}
	s.Require().NoError(s.jobs.JudgeRates(s.T().Context()))
	paused := s.outboxPayloads(constants.MailSendingPaused)
	s.Require().Len(paused, 1)
	s.Equal("bounce_rate", paused[0]["reason"])
	s.Equal("5.00", paused[0]["bounce_rate"])
	s.Equal("Shop", paused[0]["project_name"])

	o := s.overview(p)
	s.Equal("paused", o.str("sending_state"))
	s.Equal("bounce_rate", o.obj("pause")["reason"])
	s.expectError(s.sendTo(key, "no-reply@example.com", "x@example.net"), http.StatusLocked, 54, 10001)

	for i := 30; i < 40; i++ {
		s.bounce(ids[0], 0, i)
	}
	lifted, err := s.handlers.Operations.Unpause(s.T().Context(), p.ProjectID)
	s.Require().NoError(err)
	s.True(lifted)
	s.Require().Equal(http.StatusAccepted, s.sendTo(key, "no-reply@example.com", "x@example.net").Code)

	s.clock.Advance(20 * time.Minute)
	s.Require().NoError(s.jobs.JudgeRates(s.T().Context()))
	s.Len(s.outboxPayloads(constants.MailSendingPaused), 1, "the days before the operator's decision are not judged again")
	s.Require().Equal(http.StatusAccepted, s.sendTo(key, "no-reply@example.com", "y@example.net").Code)
}

// A limit raised after midnight applies to sends made before the
// evaluation that day, not only from the next day.
func (s *mailSuite) TestLimits_ARaisedLimitAppliesTheSameDay() {
	p, key := s.ready()
	s.sendMany(key, 100)

	s.clock.Set(time.Date(2026, 10, 13, 0, 0, 10, 0, time.UTC))
	s.Require().Equal(http.StatusAccepted, s.sendTo(key, "no-reply@example.com", "early@example.net").Code)
	s.Require().NoError(s.jobs.EvaluateDays(s.T().Context()))
	s.Equal(float64(400), s.overview(p).obj("daily_limit")["limit"])

	for i := range 7 {
		s.Require().Equal(http.StatusAccepted, s.sendTo(key, "no-reply@example.com", addresses(50, fmt.Sprintf("late%d-", i))...).Code)
	}
	s.expectError(s.sendTo(key, "no-reply@example.com", addresses(50, "over-")...), http.StatusTooManyRequests, 54, 10002)
	s.Equal(float64(351), s.overview(p).obj("daily_limit")["used"], "351 of 400: the next 50 would pass it")
}

func (s *mailSuite) TestLimits_FewMessagesAreNotJudged() {
	p, key := s.ready()
	id := s.sendTo(key, "no-reply@example.com", "gone@example.net").str("id")
	s.post(s.delivery(id, "gone@example.net", "Bounce", "InvalidRecipient", 550, `{"class":5,"subject":1,"detail":1}`))

	s.Require().NoError(s.jobs.JudgeRates(s.T().Context()))
	s.Empty(s.outboxPayloads(constants.MailSendingPaused))
	s.Equal("100.00", s.overview(p).obj("rates")["bounce_rate"])
}
