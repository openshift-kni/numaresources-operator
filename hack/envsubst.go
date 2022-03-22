package main

import (
	"bufio"
	"fmt"
	"log"
	"os"

	"github.com/drone/envsubst"
)

func main() {
	stdin := bufio.NewScanner(os.Stdin)

	for stdin.Scan() {
		line, err := envsubst.Eval(stdin.Text(), func(env string) string {
			if val, ok := os.LookupEnv(env); ok {
				return val
			}
			return fmt.Sprintf("${%s}", env)
		})
		if err != nil {
			log.Fatalf("Error while envsubst: %v", err)
		}
		fmt.Println(line)
	}
}
