package main

import (
	"fmt"
	"os"
)

func main() {
	os.Symlink("nonexistent", "broken_link")
	defer os.Remove("broken_link")

	info, err := os.Stat("broken_link")
	if err != nil {
		fmt.Println("Stat error:", err)
	} else {
		fmt.Println("Stat success, IsDir:", info.IsDir())
	}

	infoL, errL := os.Lstat("broken_link")
	if errL != nil {
		fmt.Println("Lstat error:", errL)
	} else {
		fmt.Println("Lstat success, IsDir:", infoL.IsDir())
	}
}
