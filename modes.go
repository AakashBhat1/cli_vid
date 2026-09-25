package main

import (
	"bytes"
	"image"
	"strconv"
)

type RenderMode int

const (
	ModeHalfBlock RenderMode = iota // 0: Crisp Half-Block TrueColor (2 pixels/cell)
	ModeASCII                       // 1: Classic Standard Colored ASCII (with Bayer Dithering)
	ModeBraille                     // 2: High-density Braille Matrix (8 dots/cell with Dithering)
	ModeMatrix                      // 3: Cyberpunk / Matrix Green Monochrome
	modeCount                       // total count
)

func (m RenderMode) Name() string {
	switch m {
	case ModeHalfBlock:
		return "Half-block TrueColor (HD)"
	case ModeASCII:
		return "Standard Colored ASCII (Dithered)"
	case ModeBraille:
		return "Braille Matrix (High-Def)"
	case ModeMatrix:
		return "Matrix Green Monochrome"
	default:
		return "Unknown"
	}
}

func (m RenderMode) ShortName() string {
	switch m {
	case ModeHalfBlock:
		return "BLOCK"
	case ModeASCII:
		return "ASCII"
	case ModeBraille:
		return "BRAILLE"
	case ModeMatrix:
		return "MATRIX"
	default:
		return "UNKNOWN"
	}
}

func (m RenderMode) PixelAspect() float64 {
	if m == ModeASCII || m == ModeMatrix {
		return 0.5
	}
	return 1
}

// TargetDimensions returns the required pixel width and height for a given terminal grid
func (m RenderMode) TargetDimensions(termW, termH int) (int, int) {
	if termW < 1 {
		termW = 1
	}
	if termH < 1 {
		termH = 1
	}

	switch m {
	case ModeHalfBlock:
		return termW, termH * 2
	case ModeASCII:
		return termW, termH
	case ModeBraille:
		return termW * 2, termH * 4
	case ModeMatrix:
		return termW, termH
	default:
		return termW, termH
	}
}

var asciiRampDetailed = []rune(" .'`^\",:;Il!i><~+_-?][}{1)(|\\/tfjrxnuvczXYUJCLQ0OZmwqpdbkhao*#MW&8%B@$")
var matrixRamp = []rune(" .0123456789ABCDEFｦｱｳｴｵｶｷｹｺｻｼｽｾｿﾀﾂﾃﾅﾆﾇﾈﾊﾋﾎﾏﾐﾑﾒﾓﾔﾕﾗﾘﾜ#*+")

// 4x4 Bayer Dithering Matrix for smooth photographic shading
var bayerMatrix4x4 = [4][4]int{
	{0, 8, 2, 10},
	{12, 4, 14, 6},
	{3, 11, 1, 9},
	{15, 7, 13, 5},
}

// Fast Zero-Allocation ANSI color formatting helpers
func appendFgRGB(buf *bytes.Buffer, r, g, b int) {
	var scratch [3]byte
	buf.WriteString("\x1b[38;2;")
	buf.Write(strconv.AppendInt(scratch[:0], int64(r), 10))
	buf.WriteByte(';')
	buf.Write(strconv.AppendInt(scratch[:0], int64(g), 10))
	buf.WriteByte(';')
	buf.Write(strconv.AppendInt(scratch[:0], int64(b), 10))
	buf.WriteByte('m')
}

func appendBgRGB(buf *bytes.Buffer, r, g, b int) {
	var scratch [3]byte
	buf.WriteString("\x1b[48;2;")
	buf.Write(strconv.AppendInt(scratch[:0], int64(r), 10))
	buf.WriteByte(';')
	buf.Write(strconv.AppendInt(scratch[:0], int64(g), 10))
	buf.WriteByte(';')
	buf.Write(strconv.AppendInt(scratch[:0], int64(b), 10))
	buf.WriteByte('m')
}

// RenderFrame converts raw RGBA frame pixels to ANSI terminal output buffer with zero-allocation serialization
func RenderFrame(buf *bytes.Buffer, img *image.RGBA, termW, termH int, mode RenderMode) {
	buf.WriteString("\x1b[40m")
	switch mode {
	case ModeHalfBlock:
		renderHalfBlock(buf, img, termW, termH)
	case ModeASCII:
		renderASCII(buf, img, termW, termH)
	case ModeBraille:
		renderBraille(buf, img, termW, termH)
	case ModeMatrix:
		renderMatrix(buf, img, termW, termH)
	}
}

// renderHalfBlock renders 2 vertical pixels per character cell using '▀', with color deduplication
func renderHalfBlock(buf *bytes.Buffer, img *image.RGBA, termW, termH int) {
	bounds := img.Bounds()
	imgW := bounds.Dx()
	imgH := bounds.Dy()

	var lastFgR, lastFgG, lastFgB int = -1, -1, -1
	var lastBgR, lastBgG, lastBgB int = -1, -1, -1

	for y := 0; y < termH; y++ {
		topY := y * 2
		botY := topY + 1

		for x := 0; x < termW; x++ {
			if x >= imgW {
				buf.WriteByte(' ')
				continue
			}

			// Sample top pixel
			topOffset := topY*img.Stride + x*4
			var tr, tg, tb int
			if topY < imgH {
				tr = int(img.Pix[topOffset])
				tg = int(img.Pix[topOffset+1])
				tb = int(img.Pix[topOffset+2])
			}

			// Sample bottom pixel
			var br, bg, bb int
			if botY < imgH {
				botOffset := botY*img.Stride + x*4
				br = int(img.Pix[botOffset])
				bg = int(img.Pix[botOffset+1])
				bb = int(img.Pix[botOffset+2])
			}

			// Optimization: If top and bottom colors are identical (within small threshold),
			// emit ' ' with background color to eliminate foreground escape overhead!
			diffR := tr - br
			diffG := tg - bg
			diffB := tb - bb
			if diffR < 0 {
				diffR = -diffR
			}
			if diffG < 0 {
				diffG = -diffG
			}
			if diffB < 0 {
				diffB = -diffB
			}

			if diffR <= 4 && diffG <= 4 && diffB <= 4 {
				if br != lastBgR || bg != lastBgG || bb != lastBgB {
					appendBgRGB(buf, br, bg, bb)
					lastBgR, lastBgG, lastBgB = br, bg, bb
				}
				buf.WriteByte(' ')
				continue
			}

			// Separate top and bottom colors
			if tr != lastFgR || tg != lastFgG || tb != lastFgB {
				appendFgRGB(buf, tr, tg, tb)
				lastFgR, lastFgG, lastFgB = tr, tg, tb
			}
			if br != lastBgR || bg != lastBgG || bb != lastBgB {
				appendBgRGB(buf, br, bg, bb)
				lastBgR, lastBgG, lastBgB = br, bg, bb
			}

			buf.WriteString("▀")
		}
		// Reset colors at end of line
		buf.WriteString("\x1b[0m\x1b[40m\r\n")
		lastFgR, lastFgG, lastFgB = -1, -1, -1
		lastBgR, lastBgG, lastBgB = -1, -1, -1
	}
}

// renderASCII renders characters mapped by luminance with Bayer dithering and TrueColor foreground
func renderASCII(buf *bytes.Buffer, img *image.RGBA, termW, termH int) {
	bounds := img.Bounds()
	imgW := bounds.Dx()
	imgH := bounds.Dy()
	rampLen := len(asciiRampDetailed)

	var lastR, lastG, lastB int = -1, -1, -1

	for y := 0; y < termH; y++ {
		if y >= imgH {
			buf.WriteByte('\n')
			continue
		}
		rowOffset := y * img.Stride

		for x := 0; x < termW; x++ {
			if x >= imgW {
				buf.WriteByte(' ')
				continue
			}

			offset := rowOffset + x*4
			r := int(img.Pix[offset])
			g := int(img.Pix[offset+1])
			b := int(img.Pix[offset+2])

			lum := (299*r + 587*g + 114*b) / 1000

			// 4x4 Bayer Dithering for smooth continuous gradient shading
			ditherVal := (bayerMatrix4x4[y%4][x%4] - 8) * 3
			ditheredLum := lum + ditherVal
			if ditheredLum < 0 {
				ditheredLum = 0
			} else if ditheredLum > 255 {
				ditheredLum = 255
			}

			idx := (ditheredLum * (rampLen - 1)) / 255
			char := asciiRampDetailed[idx]

			if r != lastR || g != lastG || b != lastB {
				appendFgRGB(buf, r, g, b)
				lastR, lastG, lastB = r, g, b
			}
			buf.WriteRune(char)
		}
		buf.WriteString("\x1b[0m\x1b[40m\r\n")
		lastR, lastG, lastB = -1, -1, -1
	}
}

// renderBraille renders a 2x4 dot matrix per character cell with Bayer dithering for ultra-crisp detail
func renderBraille(buf *bytes.Buffer, img *image.RGBA, termW, termH int) {
	bounds := img.Bounds()
	imgW := bounds.Dx()
	imgH := bounds.Dy()

	var lastR, lastG, lastB int = -1, -1, -1

	for y := 0; y < termH; y++ {
		baseY := y * 4

		for x := 0; x < termW; x++ {
			baseX := x * 2

			var code rune = 0x2800
			var sumR, sumG, sumB, count int

			// Dot 1: (0, 0)
			if checkDitheredDot(img, baseX, baseY, imgW, imgH, &sumR, &sumG, &sumB, &count) {
				code |= 0x01
			}
			// Dot 2: (0, 1)
			if checkDitheredDot(img, baseX, baseY+1, imgW, imgH, &sumR, &sumG, &sumB, &count) {
				code |= 0x02
			}
			// Dot 3: (0, 2)
			if checkDitheredDot(img, baseX, baseY+2, imgW, imgH, &sumR, &sumG, &sumB, &count) {
				code |= 0x04
			}
			// Dot 4: (1, 0)
			if checkDitheredDot(img, baseX+1, baseY, imgW, imgH, &sumR, &sumG, &sumB, &count) {
				code |= 0x08
			}
			// Dot 5: (1, 1)
			if checkDitheredDot(img, baseX+1, baseY+1, imgW, imgH, &sumR, &sumG, &sumB, &count) {
				code |= 0x10
			}
			// Dot 6: (1, 2)
			if checkDitheredDot(img, baseX+1, baseY+2, imgW, imgH, &sumR, &sumG, &sumB, &count) {
				code |= 0x20
			}
			// Dot 7: (0, 3)
			if checkDitheredDot(img, baseX, baseY+3, imgW, imgH, &sumR, &sumG, &sumB, &count) {
				code |= 0x40
			}
			// Dot 8: (1, 3)
			if checkDitheredDot(img, baseX+1, baseY+3, imgW, imgH, &sumR, &sumG, &sumB, &count) {
				code |= 0x80
			}

			if count > 0 {
				avgR := sumR / count
				avgG := sumG / count
				avgB := sumB / count

				if avgR != lastR || avgG != lastG || avgB != lastB {
					appendFgRGB(buf, avgR, avgG, avgB)
					lastR, lastG, lastB = avgR, avgG, avgB
				}
				buf.WriteRune(code)
			} else {
				buf.WriteByte(' ')
			}
		}
		buf.WriteString("\x1b[0m\x1b[40m\r\n")
		lastR, lastG, lastB = -1, -1, -1
	}
}

func checkDitheredDot(img *image.RGBA, x, y, w, h int, sumR, sumG, sumB, count *int) bool {
	if x >= w || y >= h {
		return false
	}
	offset := y*img.Stride + x*4
	r := int(img.Pix[offset])
	g := int(img.Pix[offset+1])
	b := int(img.Pix[offset+2])

	lum := (299*r + 587*g + 114*b) / 1000

	// Adaptive threshold with Bayer dithering
	ditherVal := (bayerMatrix4x4[y%4][x%4] - 8) * 7
	threshold := 85 + ditherVal

	if lum > threshold {
		*sumR += r
		*sumG += g
		*sumB += b
		*count++
		return true
	}
	return false
}

// renderMatrix renders cyberpunk green monochrome phosphor CRT styling
func renderMatrix(buf *bytes.Buffer, img *image.RGBA, termW, termH int) {
	bounds := img.Bounds()
	imgW := bounds.Dx()
	imgH := bounds.Dy()
	rampLen := len(matrixRamp)

	var lastR, lastG, lastB int = -1, -1, -1

	for y := 0; y < termH; y++ {
		if y >= imgH {
			buf.WriteByte('\n')
			continue
		}
		rowOffset := y * img.Stride

		for x := 0; x < termW; x++ {
			if x >= imgW {
				buf.WriteByte(' ')
				continue
			}

			offset := rowOffset + x*4
			r := int(img.Pix[offset])
			g := int(img.Pix[offset+1])
			b := int(img.Pix[offset+2])

			lum := (299*r + 587*g + 114*b) / 1000

			// Color grading for Matrix Green Phosphor
			var mr, mg, mb int
			if lum < 35 {
				mr, mg, mb = 0, 0, 0
			} else if lum < 120 {
				mr = 0
				mg = lum * 14 / 10
				mb = 20
			} else if lum < 210 {
				mr = (lum - 120) * 4 / 10
				mg = 255
				mb = (lum - 120) * 5 / 10
			} else {
				mr = 190 + (lum-210)*65/45
				mg = 255
				mb = 190 + (lum-210)*65/45
			}

			idx := (lum * (rampLen - 1)) / 255
			char := matrixRamp[idx]

			if mr != lastR || mg != lastG || mb != lastB {
				appendFgRGB(buf, mr, mg, mb)
				lastR, lastG, lastB = mr, mg, mb
			}
			buf.WriteRune(char)
		}
		buf.WriteString("\x1b[0m\x1b[40m\r\n")
		lastR, lastG, lastB = -1, -1, -1
	}
}
