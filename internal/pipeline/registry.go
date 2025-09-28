package pipeline

type Factory func() Pipeline

type Registry struct{ tasks map[string]Factory }

func NewRegistry() *Registry { return &Registry{tasks: map[string]Factory{}} }

func (r *Registry) Register(name string, factory Factory) { r.tasks[name] = factory }

func (r *Registry) Names() []string {
	out := make([]string, 0, len(r.tasks))
	for k := range r.tasks {
		out = append(out, k)
	}
	return out
}

func (r *Registry) Create(name string) (Pipeline, bool) {
	f, ok := r.tasks[name]
	if !ok {
		return nil, false
	}
	return f(), true
}
