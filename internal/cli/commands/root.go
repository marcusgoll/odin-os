package commands

type RootCommand struct {
	Name string
	Args []string
}

func ParseRoot(args []string) RootCommand {
	if len(args) == 0 {
		return RootCommand{Name: "help"}
	}
	return RootCommand{
		Name: args[0],
		Args: args[1:],
	}
}
