package main

import (
	"github.com/spf13/cobra"
)

func main() {
	command := cobra.Command{
		Use:   "timetrack",
		Short: "Tool for tracking your work time",
	}
	err := command.Execute()
	if err != nil {
		panic(err)
	}
}
