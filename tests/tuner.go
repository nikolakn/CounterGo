package main

import (
	"bufio"
	"fmt"
	"log"
	"math"
	"os"
	"runtime"
	"sync"
	"sync/atomic"

	"github.com/ChizhovVadim/CounterGo/common"
)

type TunableEvaluator interface {
	Evaluate(p *common.Position) int
	Apply(weights []int) []int
}

type Tuner struct {
	Logger      *log.Logger
	EvalBuilder func() TunableEvaluator
	FilePath    string
	Lambda      float64
	samples     []tuneEntry
	threads     []tunerThread
}

type tuneEntry struct {
	score    float64
	position common.Position
}

type tunerThread struct {
	evaluator TunableEvaluator
	sum       float64
}

func (t *Tuner) Run() error {
	t.Logger.Println("Tune started.")
	defer t.Logger.Println("Tune finished.")

	var err = t.readSamples()
	if err != nil {
		return err
	}

	t.Logger.Printf("Loaded %v entries", len(t.samples))

	t.threads = make([]tunerThread, runtime.NumCPU())
	for i := range t.threads {
		t.threads[i].evaluator = t.EvalBuilder()
	}

	var evalService = t.EvalBuilder()
	var weights = evalService.Apply(nil)
	t.Logger.Printf("Params count: %v", len(weights))
	t.coordinateDescent(weights)

	var er = t.computeError(weights)
	fmt.Printf("// Error: %.6f\n", er)
	fmt.Printf("var autoGeneratedWeights = %#v\n", weights)

	return nil
}

func (t *Tuner) readSamples() error {
	file, err := os.Open(t.FilePath)
	if err != nil {
		return err
	}
	defer file.Close()
	var scanner = bufio.NewScanner(file)
	t.samples = nil
	for scanner.Scan() {
		var line = scanner.Text()
		var entry, err = parseLearnEntry(line)
		if err != nil {
			return err
		}
		p, err := common.NewPositionFromFEN(entry.fen)
		if err != nil {
			return err
		}
		if p.IsCheck() {
			continue
		}
		t.samples = append(t.samples,
			tuneEntry{
				score:    entry.score,
				position: p,
			})
	}
	return scanner.Err()
}

func (t *Tuner) regularization(weights []int) float64 {
	var sum = 0.0
	for _, w := range weights {
		sum += math.Abs(float64(w))
	}
	return sum * t.Lambda
}

func (t *Tuner) computeError(weights []int) float64 {
	var wg = &sync.WaitGroup{}
	var index = int32(-1)
	for i := range t.threads {
		wg.Add(1)
		go func(thread *tunerThread) {
			thread.sum = 0
			thread.evaluator.Apply(weights)
			for {
				var i = int(atomic.AddInt32(&index, 1))
				if i >= len(t.samples) {
					break
				}
				var entry = &t.samples[i]
				var eval = thread.evaluator.Evaluate(&entry.position)
				if !entry.position.WhiteMove {
					eval = -eval
				}
				var diff = entry.score - sigmoid(float64(eval))
				thread.sum += diff * diff
			}
			wg.Done()
		}(&t.threads[i])
	}
	wg.Wait()
	var sum = 0.0
	for i := range t.threads {
		sum += t.threads[i].sum
	}
	return sum/float64(len(t.samples)) + t.regularization(weights)
}

func (t *Tuner) coordinateDescent(weights []int) {
	var bestE = t.computeError(weights)

	var steps = make([]int, len(weights))
	for i := range steps {
		steps[i] = 1
	}

	var breakF = shouldBreak(3, 0.00004)

	for iteration := 1; ; iteration++ {
		if breakF(bestE) {
			break
		}

		for weightIndex := range weights {
			var oldStep = steps[weightIndex]
			var oldValue = weights[weightIndex]
			var newValue = oldValue + oldStep
			weights[weightIndex] = newValue
			var e = t.computeError(weights)
			if e < bestE {
				// improved
				bestE = e
				steps[weightIndex] *= 2
			} else {
				weights[weightIndex] = oldValue
				if oldStep > 0 {
					steps[weightIndex] = -1
				} else {
					steps[weightIndex] = 1
				}
			}
		}

		t.Logger.Printf("Iteration: %v Error: %.6f Params: %#v",
			iteration, bestE, weights)
	}
}

func sigmoid(s float64) float64 {
	return 1.0 / (1.0 + math.Exp(-s/135))
}

func shouldBreak(emaPeriod int, stopErrChange float64) func(err float64) bool {
	var iteration = -1
	var ema float64
	var prevErr float64
	return func(err float64) bool {
		iteration++
		if iteration == 0 {
			prevErr = err
			return false
		}
		var errChange = prevErr - err
		prevErr = err
		if iteration == 1 {
			ema = errChange
			return false
		}
		ema += (errChange - ema) / float64(emaPeriod)
		return ema < stopErrChange
	}
}
