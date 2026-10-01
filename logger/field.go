package logger

type Fields map[string]any

func NewFields() Fields {
	return make(Fields)
}

func (f Fields) Add(key string, value any) Fields {
	f[key] = value

	return f
}
