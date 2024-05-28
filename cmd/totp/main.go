package main

import (
	"fmt"
	"image"
	"log"

	"github.com/pquerna/otp/totp"
)

func main() {
	key, err := totp.Generate(totp.GenerateOpts{
		AccountName: "dev6699",
		Issuer:      "rterm",
	})
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println("Secret: ", key.Secret())

	img, err := key.Image(45, 45)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("QR Code:")
	printQR(img)
}

func printQR(img image.Image) {
	bounds := img.Bounds()
	width := bounds.Max.X
	height := bounds.Max.Y

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			color := img.At(x, y)
			r, g, b, _ := color.RGBA()
			grayscale := (r + g + b) / 3
			if grayscale > 0x7FFF {
				fmt.Print("  ")
			} else {
				fmt.Print("██")
			}
		}
		fmt.Print("\n")
	}
}
