package oauth

// HoldTheConsumedCode makes every exchange that spends a code run hold before it goes on -- inside its
// transaction, with the code's row lock held -- so a test can make the "a concurrent presentation is
// not a reuse" branch (ConsumeOAuthCode's reached=false) deterministic.
func (s *Server) HoldTheConsumedCode(hold func()) { s.afterConsume = hold }
