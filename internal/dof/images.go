package dof

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
)

// bitmap is one image frame, indexed [x][y].
type bitmap struct {
	w, h int
	px   [][]RGBA
}

type imageData struct{ frames []*bitmap }

func (b *bitmap) at(x, y int) RGBA {
	if x < 0 || y < 0 || x >= b.w || y >= b.h {
		return RGBA{}
	}
	return b.px[x][y]
}

func newBitmap(img image.Image) *bitmap {
	r := img.Bounds()
	b := &bitmap{w: r.Dx(), h: r.Dy()}
	b.px = make([][]RGBA, b.w)
	for x := 0; x < b.w; x++ {
		b.px[x] = make([]RGBA, b.h)
		for y := 0; y < b.h; y++ {
			c := color.NRGBAModel.Convert(img.At(r.Min.X+x, r.Min.Y+y)).(color.NRGBA)
			b.px[x][y] = RGBA{int(c.R), int(c.G), int(c.B), int(c.A)}
		}
	}
	return b
}

// loadImageFile decodes png/jpeg (one frame) or gif (all frames).
func loadImageFile(path string) (*imageData, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if strings.EqualFold(filepath.Ext(path), ".gif") {
		g, err := gif.DecodeAll(f)
		if err != nil {
			return nil, err
		}
		out := &imageData{}
		canvas := image.NewNRGBA(image.Rect(0, 0, g.Config.Width, g.Config.Height))
		for _, fr := range g.Image {
			draw.Draw(canvas, fr.Bounds(), fr, fr.Bounds().Min, draw.Over)
			snap := image.NewNRGBA(canvas.Bounds())
			copy(snap.Pix, canvas.Pix)
			out.frames = append(out.frames, newBitmap(snap))
		}
		return out, nil
	}
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &imageData{frames: []*bitmap{newBitmap(img)}}, nil
}

// clip extracts a region and scales it to resW x resH (DOF FastBitmap.GetClip).
func (b *bitmap) clip(resW, resH, srcLeft, srcTop, srcW, srcH int, mode string) [][]RGBA {
	if resW < 0 {
		resW = 0
	}
	if resH < 0 {
		resH = 0
	}
	srcLeft = limit(srcLeft, 0, max(b.w-1, 0))
	srcTop = limit(srcTop, 0, max(b.h-1, 0))
	if srcW < 0 {
		srcW = b.w - srcLeft
	}
	if srcH < 0 {
		srcH = b.h - srcTop
	}
	out := make([][]RGBA, resW)
	for x := range out {
		out[x] = make([]RGBA, resH)
	}
	if resW == 0 || resH == 0 {
		return out
	}
	psw := float64(srcW) / float64(resW)
	psh := float64(srcH) / float64(resH)
	if strings.EqualFold(mode, "BlendPixels") || mode == "" {
		count := psw * psh
		for y := 0; y < resH; y++ {
			y0 := float64(srcTop) + float64(y)*psh
			y1 := y0 + psh
			for x := 0; x < resW; x++ {
				x0 := float64(srcLeft) + float64(x)*psw
				x1 := x0 + psw
				var r, g, bl, a float64
				for sy := int(math.Floor(y0)); float64(sy) < y1; sy++ {
					wy := math.Min(float64(sy+1), y1) - math.Max(float64(sy), y0)
					if wy <= 0 {
						continue
					}
					for sx := int(math.Floor(x0)); float64(sx) < x1; sx++ {
						wx := math.Min(float64(sx+1), x1) - math.Max(float64(sx), x0)
						if wx <= 0 {
							continue
						}
						p := b.at(sx, sy)
						w := wx * wy
						r += float64(p.R) * w
						g += float64(p.G) * w
						bl += float64(p.B) * w
						a += float64(p.A) * w
					}
				}
				cl := func(v float64) int { return limit(int(v/count), 0, 255) }
				out[x][y] = RGBA{cl(r), cl(g), cl(bl), cl(a)}
			}
		}
		return out
	}
	xs0, ys0 := 0.0, 0.0
	if strings.EqualFold(mode, "SinglePixelCenter") {
		xs0, ys0 = psw/2, psh/2
	}
	ysrc := ys0
	for y := 0; y < resH; y++ {
		xsrc := xs0
		for x := 0; x < resW; x++ {
			out[x][y] = b.at(int(math.Round(xsrc)), int(math.Round(ysrc)))
			xsrc += psw
		}
		ysrc += psh
	}
	return out
}

func (e *Engine) loadImage(path string) (*imageData, error) {
	if img, ok := e.images[path]; ok {
		return img, nil
	}
	img, err := loadImageFile(path)
	if err != nil {
		return nil, err
	}
	if e.images == nil {
		e.images = map[string]*imageData{}
	}
	e.images[path] = img
	return img, nil
}

// ---------------------------------------------------------------- shapes

// ShapeDef is one entry of DirectOutputShapes.xml.
type ShapeDef struct {
	Name                 string
	Frame, Top, Left     int
	Width, Height        int
	Mode                 string
	Animated             bool
	StepDir              byte
	StepSize, FrameCount int
	FrameMs              int
	Behaviour            byte
}

// ShapeDefs holds the shape definitions and the bitmap they refer to.
type ShapeDefs struct {
	BitmapFile string
	Shapes     map[string]*ShapeDef
}

// LoadShapeDefs reads DirectOutputShapes.xml (the bitmap is the .png next to it).
func LoadShapeDefs(path string) (*ShapeDefs, error) {
	root, err := readXML(path)
	if err != nil {
		return nil, err
	}
	sd := &ShapeDefs{Shapes: map[string]*ShapeDef{}}
	png := strings.TrimSuffix(path, filepath.Ext(path)) + ".png"
	sd.BitmapFile = png
	shapes := root.child("Shapes")
	if shapes == nil {
		return sd, nil
	}
	for _, s := range shapes.Children {
		d := &ShapeDef{Name: s.text("Name"), Width: -1, Height: -1, Mode: "BlendPixels", StepDir: 'F', StepSize: 1, FrameCount: 1, FrameMs: 30, Behaviour: 'L'}
		d.Frame = toInt(s.text("BitmapFrameNumber"))
		d.Top = toInt(s.text("BitmapTop"))
		d.Left = toInt(s.text("BitmapLeft"))
		if v := s.text("BitmapWidth"); v != "" {
			d.Width = toInt(v)
		}
		if v := s.text("BitmapHeight"); v != "" {
			d.Height = toInt(v)
		}
		if v := s.text("DataExtractMode"); v != "" {
			d.Mode = v
		}
		if s.Name == "ShapeAnimated" {
			d.Animated = true
			if v := s.text("AnimationStepDirection"); v != "" {
				d.StepDir = strings.ToUpper(v)[0]
			}
			if v := s.text("AnimationStepSize"); v != "" {
				d.StepSize = toInt(v)
			}
			if v := s.text("AnimationFrameCount"); v != "" {
				d.FrameCount = max(toInt(v), 1)
			}
			if v := s.text("AnimationFrameDurationMs"); v != "" {
				d.FrameMs = max(toInt(v), 1)
			}
			if v := s.text("AnimationBehaviour"); v != "" {
				d.Behaviour = strings.ToUpper(v)[0]
			}
		}
		if d.Name != "" {
			sd.Shapes[strings.ToUpper(d.Name)] = d
		}
	}
	return sd, nil
}
