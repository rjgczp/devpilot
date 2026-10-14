package registration

import "github.com/rjgczp/devpilot/internal/credentials"

const MaxBodyBytes = credentials.MaxBodyBytes

type input = credentials.Input

func parseInput(body []byte) (input, error) { return credentials.Parse(body) }
func validate(in *input) map[string]string  { return credentials.Validate(in, 15) }
func validEmail(email string) bool          { return credentials.ValidEmail(email) }
