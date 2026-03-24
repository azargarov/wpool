package workerpool


type segWord 	  uint64
type segState     uint8
type segReserve   uint32
type segGen       uint32


const (
	segOpen 	segState = iota
	segClosed
    segDetached
)

//| 8 bit state| 24 bit reserve | 32 bit gen |

const (
	stateBits   = 8
	reserveBits = 24
	genBits     = 32

	genShift     = 0
	reserveShift = genShift + genBits
	stateShift   = reserveShift + reserveBits

	genMask     = ( (uint64(1)<<genBits) - 1) << genShift
	reserveMask = ( (uint64(1)<<reserveBits) - 1) << reserveShift
	stateMask   = ( (uint64(1)<<stateBits) - 1) << stateShift

	reserveStep = segWord(1) << reserveShift
)


func (w segWord) state() segState {
	return segState((uint64(w) & stateMask) >> stateShift)
}

func (w segWord) reserve() segReserve {
	return segReserve((uint64(w) & reserveMask) >> reserveShift)
}

func (w segWord) gen() segGen {
	return segGen((uint64(w) & genMask) >> genShift)
}

func pack(state segState, reserve segReserve, gen segGen) segWord {

	if uint32(reserve) >= (1 << reserveBits) {
		panic("reserve overflow")
	}

	return segWord(
		(uint64(state) << stateShift) |
		(uint64(reserve) << reserveShift) |
		(uint64(gen) << genShift),
	)
}

func withReserve(w segWord, r segReserve) segWord {
	return segWord(
		(uint64(w) &^ reserveMask) |
		(uint64(r) << reserveShift),
	)
}

func withState(w segWord, s segState) segWord {
	return segWord(
		(uint64(w) &^ stateMask) |
		(uint64(s) << stateShift),
	)
}

func withGen(w segWord, s segGen) segWord{
	return segWord(
		(uint64(w) &^ genMask) |
		(uint64(s) << genShift),
	)
}


// helpers

func incReserve(w segWord) segWord {
	return w + reserveStep
}

func segToClosed(w segWord) (segWord) {
	if w.state() != segOpen{
		return w
	}
	return withState(w, segClosed)
}

func segToDetach(w segWord) segWord{
	return withState(w,segDetached)
}

func nextGen(g segGen) segGen {
	g++
	if g == 0 {
		g = 1
	}
	return g
}

func segReset(w segWord) segWord {
	return pack(segOpen, 0, nextGen(w.gen()))
}