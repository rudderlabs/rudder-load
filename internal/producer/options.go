package producer

type config struct {
	validate func(headers map[string]string, statusCode int, body []byte) error
}

type Option func(*config)

func WithValidate(f func(_ map[string]string, _ int, _ []byte) error) Option {
	return func(c *config) { c.validate = f }
}
