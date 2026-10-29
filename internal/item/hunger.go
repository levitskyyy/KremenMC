package item

// Hunger is the player food state (FoodData).
type Hunger struct {
	Food       int32
	Saturation float32
	Exhaustion float32
	timer      int32
}

// Difficulty ids.
const (
	Peaceful int32 = iota
	Easy
	Normal
	Hard
)

func NewHunger() Hunger { return Hunger{Food: 20, Saturation: 5} }

func (h *Hunger) AddExhaustion(x float32) { h.Exhaustion = min(h.Exhaustion+x, 40) }

func (h *Hunger) NeedsFood() bool { return h.Food < 20 }

// Eat adds a food component's nutrition and saturation.
func (h *Hunger) Eat(f FoodInfo) {
	h.Food = min(max(f.Nutrition+h.Food, 0), 20)
	h.Saturation = min(max(f.Saturation+h.Saturation, 0), float32(h.Food))
}

// Tick runs one FoodData.tick. It returns the health to restore and
// whether starvation damage (1) applies this tick.
func (h *Hunger) Tick(difficulty int32, naturalRegen bool, health, maxHealth float32) (heal float32, starve bool) {
	if h.Exhaustion > 4 {
		h.Exhaustion -= 4
		if h.Saturation > 0 {
			h.Saturation = max(h.Saturation-1, 0)
		} else if difficulty != Peaceful {
			h.Food = max(h.Food-1, 0)
		}
	}
	hurt := health > 0 && health < maxHealth
	switch {
	case naturalRegen && h.Saturation > 0 && hurt && h.Food >= 20:
		if h.timer++; h.timer >= 10 {
			f := min(h.Saturation, 6)
			heal = f / 6
			h.AddExhaustion(f)
			h.timer = 0
		}
	case naturalRegen && h.Food >= 18 && hurt:
		if h.timer++; h.timer >= 80 {
			heal = 1
			h.AddExhaustion(6)
			h.timer = 0
		}
	case h.Food <= 0:
		if h.timer++; h.timer >= 80 {
			starve = health > 10 || difficulty == Hard || health > 1 && difficulty == Normal
			h.timer = 0
		}
	default:
		h.timer = 0
	}
	return heal, starve
}
