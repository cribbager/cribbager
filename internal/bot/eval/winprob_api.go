package eval

// pegJoint is one cell of the per-deal joint pegging distribution: dealer
// pegging points, pone pegging points, and the probability of that pair.
type pegJoint struct {
	D, P int
	W    float32
}

// WinProb is P(this player wins the game) at the start of a deal, from the
// self-play + DP table (winprob.go). Scores at or past the target clamp to
// certainty. It is also used as the mid-deal continuation estimate — an
// accepted approximation (the table is defined at deal boundaries).
func WinProb(myScore, oppScore int, iAmDealer bool) float64 {
	if myScore >= 121 {
		return 1
	}
	if oppScore >= 121 {
		return 0
	}
	if iAmDealer {
		return float64(winProbDealer[myScore][oppScore])
	}
	return 1 - float64(winProbDealer[oppScore][myScore])
}

// farNeed[role] is the largest points a player in that role (0 pone, 1 dealer)
// can possibly gain in one deal under the outcome model — his heels, the
// largest observed pegging for the role, and maximal hand and crib scores.
// When BOTH players need more than their farNeed, nobody can cross this deal,
// P(win) is effectively affine in points, and the win objective provably
// agrees with the points-EV objective — the fast path.
var farNeed = [2]int{}

func init() {
	maxPegD, maxPegP := 0, 0
	for _, c := range pegJointDist {
		if c.D > maxPegD {
			maxPegD = c.D
		}
		if c.P > maxPegP {
			maxPegP = c.P
		}
	}
	farNeed[0] = maxPegP + 29     // pone: pegging + hand
	farNeed[1] = 2 + maxPegD + 58 // dealer: heels + pegging + hand + crib
}

// farFromEnd reports whether neither player can reach the target this deal, so
// the win objective can defer to points EV.
func farFromEnd(myScore, oppScore int, iAmDealer bool) bool {
	myRole, oppRole := 0, 1
	if iAmDealer {
		myRole, oppRole = 1, 0
	}
	return 121-myScore > farNeed[myRole] && 121-oppScore > farNeed[oppRole]
}
