// Package recoverycursor detects repeated server-cursor traversal across batches without owning a queue,
// assigning authority or deciding operation completion.
// Package recoverycursor обнаруживает повторный обход server cursor между batches без хранения очереди,
// назначения authority и решения о завершении операции.
package recoverycursor

// Cycle retains one anchor and a doubling window to detect repeated server-cursor traversal with constant
// cursor state; callers serialize access and validate tokens, and it owns neither work nor authority.
// Cycle хранит один anchor и удваиваемое окно для обнаружения повторного обхода server cursor с постоянным
// cursor state; caller сериализует доступ и проверяет tokens, work и authority здесь не хранятся.
type Cycle struct {
	anchor   string
	power    uint64
	distance uint64
}

// Advance accepts the first token, rejects equality with the current anchor and periodically moves that
// anchor with overflow-safe window doubling; it does not validate token syntax or require lexical order.
// Advance принимает первый token, отклоняет равенство current anchor и периодически сдвигает anchor с
// overflow-safe удвоением окна; token syntax и lexical order не проверяет.
func (cycle *Cycle) Advance(next string) bool {
	if cycle.power == 0 {
		cycle.anchor, cycle.power = next, 1
		return true
	}
	if next == cycle.anchor {
		return false
	}
	cycle.distance++
	if cycle.distance == cycle.power {
		cycle.anchor, cycle.distance = next, 0
		if cycle.power <= ^uint64(0)/2 {
			cycle.power *= 2
		}
	}
	return true
}

// Reset drops the anchor/window/distance so a new traversal can start; releasing a string reference is not
// token-memory zeroization.
// Reset удаляет anchor/window/distance для нового обхода; удаление string reference не стирает память
// token.
func (cycle *Cycle) Reset() { *cycle = Cycle{} }
