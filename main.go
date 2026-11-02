package main

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"

	svg "github.com/ajstarks/svgo"
	fc "github.com/jlahd/fitcurve"
)

var xScale float64 = 2000
var yScale float64 = 1

// peakWindow is the half-width (in samples) a point must be the maximum of
// to count as a peak. curveErr is the fit tolerance in output units; it is
// large because the curve only needs to follow the general envelope.
var peakWindow = 10

// segmentSeconds is the time span covered by each fitted run of curves,
// like placing a new anchor every N seconds when tracing by hand.
var segmentSeconds = 0.02

func main() {
	args := os.Args
	inputName := "input.csv"
	outputName := "output.svg"
	for _, arg := range args {
		if strings.HasPrefix(arg, "--input=") {
			inputName, _ = strings.CutPrefix(arg, "--input=")
			if !strings.HasSuffix(inputName, ".csv") {
				inputName += ".csv"
			}
		} else if strings.HasPrefix(arg, "--output=") {
			outputName, _ = strings.CutPrefix(arg, "--output=")
			if !strings.HasSuffix(outputName, ".csv") {
				outputName += ".svg"
			}
		}
	}
	points := []fc.Point2{}

	ourBytes, err := os.ReadFile(inputName)
	if err != nil {
		panic(err)
	}

	reader := bytes.NewReader(ourBytes)
	csvReader := csv.NewReader(reader)
	csvReader.FieldsPerRecord = 2
	csvReader.TrimLeadingSpace = true

	records, err := csvReader.ReadAll()
	if err != nil {
		panic(err)
	}
	for _, record := range records {
		x, err := strconv.ParseFloat(record[0], 64)
		if err != nil {
			panic(err)
		}
		y, err := strconv.ParseFloat(record[1], 64)
		if err != nil {
			panic(err)
		}
		points = append(points, fc.Point2{x, y})
	}

	// keep only local maxima above y=0, then scale (and flip y) so the
	// error tolerance is in output units
	peaks := []fc.Point2{}
	for i, p := range points {
		if p[1] <= 0 {
			continue
		}
		isPeak := true
		for j := max(0, i-peakWindow); j <= min(len(points)-1, i+peakWindow); j++ {
			// strict on the left so plateaus yield a single peak
			if points[j][1] > p[1] || (j < i && points[j][1] == p[1]) {
				isPeak = false
				break
			}
		}
		if isPeak {
			peaks = append(peaks, fc.Point2{p[0] * xScale, -p[1] * yScale})
		}
	}

	// one anchor per segment at the average height of the peaks inside it
	// (anchors sit at the segment's mid-time), plus the true start and end.
	type seg struct{ x, y float64 }
	anchors := []seg{{peaks[0][0], peaks[0][1]}}
	for i := 0; i < len(peaks); {
		k := math.Floor(peaks[i][0] / xScale / segmentSeconds)
		var sx, sy float64
		n := 0
		for ; i < len(peaks) && math.Floor(peaks[i][0]/xScale/segmentSeconds) == k; i++ {
			sx += peaks[i][0]
			sy += peaks[i][1]
			n++
		}
		anchors = append(anchors, seg{sx / float64(n), sy / float64(n)})
	}
	last := peaks[len(peaks)-1]
	anchors = append(anchors, seg{last[0], last[1]})

	// Catmull-Rom through the anchors, converted to cubic Beziers: one curve
	// per segment with matching tangents at every join.
	curves := []fc.BezierCurve{}
	at := func(i int) seg { return anchors[max(0, min(len(anchors)-1, i))] }
	for i := 0; i+1 < len(anchors); i++ {
		p0, p1, p2, p3 := at(i-1), at(i), at(i+1), at(i+2)
		curves = append(curves, fc.BezierCurve{
			{p1.x, p1.y},
			{p1.x + (p2.x-p0.x)/4, p1.y + (p2.y-p0.y)/4},
			{p2.x - (p3.x-p1.x)/4, p2.y - (p3.y-p1.y)/4},
			{p2.x, p2.y},
		})
	}

	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, curve := range curves {
		for _, p := range curve {
			minX, maxX = math.Min(minX, p[0]), math.Max(maxX, p[0])
			minY, maxY = math.Min(minY, p[1]), math.Max(maxY, p[1])
		}
	}

	// the fill closes down to the baseline (y=0), so include it in the bounds
	minY, maxY = math.Min(minY, 0), math.Max(maxY, 0)

	var d strings.Builder
	for i, c := range curves {
		if i == 0 {
			fmt.Fprintf(&d, "M%.2f,%.2f ", c[0][0], c[0][1])
		}
		fmt.Fprintf(&d, "C%.2f,%.2f %.2f,%.2f %.2f,%.2f ",
			c[1][0], c[1][1], c[2][0], c[2][1], c[3][0], c[3][1])
	}

	end := curves[len(curves)-1][3]
	fmt.Fprintf(&d, "L%.2f,0 L%.2f,0 Z", end[0], curves[0][0][0])

	file, err := os.Create(outputName)
	if err != nil {
		panic(err)
	}

	w, h := int(math.Ceil(maxX-minX)), int(math.Ceil(maxY-minY))
	ourSVG := svg.New(file)
	ourSVG.Startview(w, h, int(math.Floor(minX)), int(math.Floor(minY)), w, h)
	ourSVG.Path(d.String(), "fill:#cde;stroke:black;stroke-width:1")
	ourSVG.End()

	if err := file.Close(); err != nil {
		panic(err)
	}
}
