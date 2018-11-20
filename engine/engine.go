package engine

import (
	"context"
	"runtime"

	. "github.com/ChizhovVadim/CounterGo/common"
)

type Engine struct {
	Hash               IntUciOption
	Threads            IntUciOption
	ExperimentSettings bool
	timeManager        timeManager
	transTable         TransTable
	lateMoveReduction  func(d, m int) int
	historyKeys        map[uint64]int
	done               <-chan struct{}
	threads            []thread
	progress           func(SearchInfo)
	mainLine           mainLine
}

type thread struct {
	engine    *Engine
	sortTable SortTable
	evaluator Evaluator
	nodes     int
	stack     [stackSize]struct {
		position       Position
		moveList       [MaxMoves]OrderedMove
		quietsSearched [MaxMoves]Move
		pv             pv
	}
}

type pv struct {
	items [stackSize]Move
	size  int
}

type mainLine struct {
	moves []Move
	score int
	depth int
}

type Evaluator interface {
	Evaluate(p *Position) int
}

type SortTable interface {
	Clear()
	Update(p *Position, bestMove Move, searched []Move, depth, height int)
	Note(p *Position, ml []OrderedMove, trans Move, height int)
	NoteQS(p *Position, ml []OrderedMove)
}

type TransTable interface {
	Megabytes() int
	PrepareNewSearch()
	Clear()
	Read(p *Position) (depth, score, bound int, move Move, ok bool)
	Update(p *Position, depth, score, bound int, move Move)
}

func NewEngine() *Engine {
	var numCPUs = runtime.NumCPU()
	return &Engine{
		Hash:               IntUciOption{Name: "Hash", Value: 4, Min: 4, Max: 512},
		Threads:            IntUciOption{Name: "Threads", Value: 1, Min: 1, Max: numCPUs},
		ExperimentSettings: false,
	}
}

func (e *Engine) GetInfo() (name, version, author string) {
	return "Counter", "3.1dev", "Vadim Chizhov"
}

func (e *Engine) GetOptions() []UciOption {
	return []UciOption{&e.Hash, &e.Threads}
}

func (e *Engine) Prepare() {
	if e.transTable == nil || e.transTable.Megabytes() != e.Hash.Value {
		e.transTable = NewTransTable(e.Hash.Value)
	}
	if e.lateMoveReduction == nil {
		e.lateMoveReduction = mainLmr
	}
	if len(e.threads) != e.Threads.Value {
		e.threads = make([]thread, e.Threads.Value)
		for i := range e.threads {
			var t = &e.threads[i]
			t.engine = e
			t.sortTable = NewSortTable()
			t.evaluator = NewEvaluationService()
		}
	}
}

func (e *Engine) Search(ctx context.Context, searchParams SearchParams) SearchInfo {
	var p = &searchParams.Positions[len(searchParams.Positions)-1]
	e.timeManager = NewTimeManager(searchParams.Limits, timeControlSmart, p.WhiteMove)
	if e.timeManager.hardTime > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, e.timeManager.hardTime)
		defer cancel()
	}
	e.Prepare()
	e.transTable.PrepareNewSearch()
	e.historyKeys = getHistoryKeys(searchParams.Positions)
	e.done = ctx.Done()
	for i := range e.threads {
		var t = &e.threads[i]
		t.nodes = 0
		t.stack[0].position = *p
	}
	e.progress = searchParams.Progress
	e.iterativeDeepening()
	return e.currentSearchResult()
}

func (e *Engine) nodes() int64 {
	var result = 0
	for i := range e.threads {
		result += e.threads[i].nodes
	}
	return int64(result)
}

func getHistoryKeys(positions []Position) map[uint64]int {
	var result = make(map[uint64]int)
	for i := len(positions) - 1; i >= 0; i-- {
		var p = &positions[i]
		result[p.Key]++
		if p.Rule50 == 0 {
			break
		}
	}
	return result
}

func (e *Engine) Clear() {
	if e.transTable != nil {
		e.transTable.Clear()
	}
	for i := range e.threads {
		var t = &e.threads[i]
		t.sortTable.Clear()
	}
}

func (ml *mainLine) update(depth, score int, mainLine []Move) {
	ml.depth = depth
	ml.score = score
	ml.moves = mainLine
}

func (e *Engine) currentSearchResult() SearchInfo {
	return SearchInfo{
		Depth:    e.mainLine.depth,
		MainLine: e.mainLine.moves,
		Score:    newUciScore(e.mainLine.score),
		Nodes:    e.nodes(),
		Time:     e.timeManager.ElapsedMilliseconds(),
	}
}

func (e *Engine) sendProgress() {
	if e.progress != nil {
		e.progress(e.currentSearchResult())
	}
}

func (pv *pv) clear() {
	pv.size = 0
}

func (pv *pv) assign(m Move, child *pv) {
	pv.size = 1 + child.size
	pv.items[0] = m
	copy(pv.items[1:], child.items[:child.size])
}

func (pv *pv) moves() []Move {
	return pv.items[:pv.size]
}
