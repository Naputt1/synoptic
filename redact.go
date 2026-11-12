package main

// redact removes every source and embedded-code field from the baked data while
// keeping docs, line numbers and file names. The page still renders its flows;
// only the raw viewer goes away.
func redact(d *JData) {
	for key, jf := range d.Flows {
		jf.Src = ""
		redactSteps(jf.Steps)
		d.Flows[key] = jf
	}
	d.Redact = true
}

func redactSteps(list []Step) {
	for i := range list {
		list[i].Code = ""
		list[i].Codes = nil
		redactSteps(list[i].Body)
		redactSteps(list[i].Else)
	}
}
