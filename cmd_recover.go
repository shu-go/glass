package main

type recoverCmd struct{}

func (c recoverCmd) Run() error {
	wins, err := listAllWindows(true, nil)
	if err != nil {
		return err
	}

	for _, w := range wins {
		setAlpha(w.Handle, 255)
	}

	return nil
}
