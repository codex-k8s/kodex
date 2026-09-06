// Package recoverycursor проверяет повтор серверного cursor между batch,
// не хранит очередь и не назначает authority или завершение операции.
package recoverycursor

// Cycle хранит одну опорную точку. Удвоение окна обнаруживает цикл произвольной
// длины с постоянной памятью, не ограничивая размер корректной очереди.
// Доступ сериализует вызывающий owner adapter.
type Cycle struct {
	anchor   string
	power    uint64
	distance uint64
}

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

func (cycle *Cycle) Reset() { *cycle = Cycle{} }
