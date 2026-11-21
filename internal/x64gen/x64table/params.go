package x64table

// Params are the names of the parameters of the encoder of a form, a choice or a sequence, in order, and whether the
// table has one of that name: what an instruction selection rule gives each one.
func Params(name string) ([]string, bool) {
	var ps []param
	found := false
	for _, f := range forms {
		if f.name == name {
			ps, found = f.params(), true
		}
	}
	for _, c := range choices {
		if c.name == name {
			ps, found = c.params(), true
		}
	}
	for _, s := range sequences {
		if s.name == name {
			ps, found = paramsOf(s.operands), true
		}
	}
	if !found {
		return nil, false
	}
	names := make([]string, len(ps))
	for i, p := range ps {
		names[i] = p.name
	}
	return names, true
}
