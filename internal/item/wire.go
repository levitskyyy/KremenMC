package item

import (
	"errors"
	"fmt"

	"github.com/levitskyyy/KremenMC/internal/proto"
)

var (
	ErrMalformed = errors.New("item: malformed component data")
	errDepth     = errors.New("item: nesting too deep")
)

const (
	maxString   = 32767
	maxList     = 1 << 16
	maxDepth    = 64
	maxPatchLen = 256
)

// skipper consumes one encoded value without materialising it.
type skipper func(r *proto.Reader, depth int) error

func fixed(n int) skipper {
	return func(r *proto.Reader, _ int) error {
		_, err := r.Raw(n)
		return err
	}
}

var (
	skNone   skipper = func(*proto.Reader, int) error { return nil }
	skBool           = fixed(1)
	skF32            = fixed(4)
	skI32            = fixed(4)
	skF64            = fixed(8)
	skI64            = fixed(8)
	skVarInt skipper = func(r *proto.Reader, _ int) error {
		_, err := r.VarInt()
		return err
	}
	skString skipper = func(r *proto.Reader, _ int) error {
		_, err := r.String(maxString)
		return err
	}
	skNBT skipper = func(r *proto.Reader, _ int) error {
		n, err := proto.NBTLen(r.B[r.Off:])
		if err != nil {
			return err
		}
		_, err = r.Raw(n)
		return err
	}
	skID   = skString
	skText = skNBT
)

func seq(s ...skipper) skipper {
	return func(r *proto.Reader, d int) error {
		for _, f := range s {
			if err := f(r, d); err != nil {
				return err
			}
		}
		return nil
	}
}

func count(r *proto.Reader, limit int) (int, error) {
	n, err := r.VarInt()
	if err != nil {
		return 0, err
	}
	if n < 0 || int(n) > limit || int(n) > r.Remaining() && limit > 0 {
		return 0, fmt.Errorf("%w: list length %d", ErrMalformed, n)
	}
	return int(n), nil
}

func list(e skipper) skipper { return listMax(e, maxList) }

func listMax(e skipper, limit int) skipper {
	return func(r *proto.Reader, d int) error {
		n, err := count(r, limit)
		if err != nil {
			return err
		}
		if d > maxDepth {
			return errDepth
		}
		for range n {
			if err := e(r, d+1); err != nil {
				return err
			}
		}
		return nil
	}
}

func repeat(n int, e skipper) skipper {
	return func(r *proto.Reader, d int) error {
		for range n {
			if err := e(r, d); err != nil {
				return err
			}
		}
		return nil
	}
}

func opt(e skipper) skipper {
	return func(r *proto.Reader, d int) error {
		ok, err := r.Bool()
		if err != nil || !ok {
			return err
		}
		return e(r, d)
	}
}

// either reads a boolean selector: true selects l.
func either(l, rt skipper) skipper {
	return func(r *proto.Reader, d int) error {
		left, err := r.Bool()
		if err != nil {
			return err
		}
		if left {
			return l(r, d)
		}
		return rt(r, d)
	}
}

// holder is a registry reference (id+1) or, for 0, an inline value.
func holder(direct skipper) skipper {
	return func(r *proto.Reader, d int) error {
		id, err := r.VarInt()
		if err != nil {
			return err
		}
		if id == 0 {
			if d > maxDepth {
				return errDepth
			}
			return direct(r, d+1)
		}
		return nil
	}
}

// skHolderSet is a tag name (0) or n-1 registry ids.
var skHolderSet skipper = func(r *proto.Reader, d int) error {
	n, err := r.VarInt()
	if err != nil {
		return err
	}
	if n == 0 {
		return skID(r, d)
	}
	if n < 0 || int(n-1) > r.Remaining() {
		return ErrMalformed
	}
	for range n - 1 {
		if _, err := r.VarInt(); err != nil {
			return err
		}
	}
	return nil
}

func dispatch(cases ...skipper) skipper {
	return func(r *proto.Reader, d int) error {
		k, err := r.VarInt()
		if err != nil {
			return err
		}
		if k < 0 || int(k) >= len(cases) || cases[k] == nil {
			return fmt.Errorf("%w: variant %d", ErrMalformed, k)
		}
		return cases[k](r, d)
	}
}

func lazy(f func() skipper) skipper {
	return func(r *proto.Reader, d int) error { return f()(r, d) }
}

var (
	skSoundEvent  = seq(skID, opt(skF32))
	skSound       = holder(skSoundEvent)
	skEffectDet   skipper
	skMobEffect   skipper
	skConsume     skipper
	skFirework    = seq(skVarInt, list(skI32), list(skI32), skBool, skBool)
	skKineticCond = seq(skVarInt, skF32, skF32)
	skTemplate    = seq(skVarInt, skVarInt, lazy(func() skipper { return skPatch }))
	skPatch       skipper
	skExactComp   skipper
	skBlockPred   skipper
	skProfile     skipper
	skEnchants    = list(seq(skVarInt, skVarInt))
	skResolveInt  = either(skI32, skID)
	skResolveF32  = either(skF32, skID)
	skDyeColor    = skVarInt
	skTrimMat     = seq(skString, list(seq(skID, skString)), skText)
	skTrimPat     = seq(skID, skVarInt, skText, skBool)
	skInstrument  = seq(skSound, skF32, skF32, skText)
	skJukeboxSong = seq(skSound, skText, skF32, skVarInt)
	skBannerPat   = seq(skID, skString)
	skPainting    = seq(skVarInt, skVarInt, skID, opt(skText), opt(skText))
)

var skippers [NumTypes]skipper

func init() {
	skEffectDet = seq(skVarInt, skVarInt, skBool, skBool, skBool, opt(lazy(func() skipper { return skEffectDet })))
	skMobEffect = seq(skVarInt, skEffectDet)
	skConsume = dispatch(
		seq(list(skMobEffect), skF32),
		skHolderSet,
		skNone,
		seq(skF32, skBool),
		skSound,
	)
	skPatch = func(r *proto.Reader, d int) error {
		if d > maxDepth {
			return errDepth
		}
		add, err := count(r, maxPatchLen)
		if err != nil {
			return err
		}
		rem, err := count(r, maxPatchLen)
		if err != nil {
			return err
		}
		for range add {
			if err := skipComponent(r, d+1); err != nil {
				return err
			}
		}
		for range rem {
			if _, err := typeOf(r); err != nil {
				return err
			}
		}
		return nil
	}
	skExactComp = func(r *proto.Reader, d int) error { return skipComponent(r, d+1) }
	prop := seq(skString, either(skString, seq(opt(skString), opt(skString))))
	partial := seq(skVarInt, skNBT)
	skBlockPred = seq(opt(skHolderSet), opt(list(prop)), opt(skNBT), list(skExactComp), list(partial))
	skProfile = profileSkipper()

	s := &skippers
	s[CustomData] = skNBT
	s[MaxStackSize] = skVarInt
	s[MaxDamage] = skVarInt
	s[Damage] = skVarInt
	s[Unbreakable] = skNone
	s[UseEffects] = seq(skBool, skBool, skF32)
	s[CustomName] = skText
	s[MinimumAttackCharge] = skF32
	s[DamageType] = holder(fixed(0))
	s[ItemName] = skText
	s[ItemModel] = skID
	s[Lore] = listMax(skText, 256)
	s[Rarity] = skVarInt
	s[Enchantments] = skEnchants
	s[CanPlaceOn] = list(skBlockPred)
	s[CanBreak] = list(skBlockPred)
	s[AttributeModifiers] = list(seq(skVarInt, skID, skF64, skVarInt, skVarInt, dispatch(skNone, skNone, skText)))
	s[CustomModelData] = seq(list(skF32), list(skBool), list(skString), list(skI32))
	s[TooltipDisplay] = seq(skBool, list(skVarInt))
	s[RepairCost] = skVarInt
	s[CreativeSlotLock] = skNone
	s[EnchantmentGlintOverride] = skBool
	s[IntangibleProjectile] = skNBT
	s[Food] = seq(skVarInt, skF32, skBool)
	s[Consumable] = seq(skF32, skVarInt, skSound, skBool, list(skConsume))
	s[UseRemainder] = skTemplate
	s[UseCooldown] = seq(skF32, opt(skID))
	s[DamageResistant] = skHolderSet
	s[Tool] = seq(list(seq(skHolderSet, opt(skF32), opt(skBool))), skF32, skVarInt, skBool)
	s[Weapon] = seq(skVarInt, skF32)
	s[AttackRange] = repeat(6, skF32)
	s[Enchantable] = skVarInt
	s[Equippable] = seq(skVarInt, skSound, opt(skID), opt(skID), opt(skHolderSet), repeat(5, skBool), skSound)
	s[Repairable] = skHolderSet
	s[Glider] = skNone
	s[TooltipStyle] = skID
	s[DeathProtection] = list(skConsume)
	s[BlocksAttacks] = seq(skF32, skF32, list(seq(skF32, opt(skHolderSet), skF32, skF32)), skF32, skF32, skF32, opt(skHolderSet), opt(skSound), opt(skSound))
	s[PiercingWeapon] = seq(skBool, skBool, opt(skSound), opt(skSound))
	s[KineticWeapon] = seq(skVarInt, skVarInt, opt(skKineticCond), opt(skKineticCond), opt(skKineticCond), skF32, skF32, opt(skSound), opt(skSound))
	s[AttackAnimation] = seq(skVarInt, skVarInt)
	s[InteractAnimation] = seq(skVarInt, skVarInt)
	s[AdditionalTradeCost] = skVarInt
	s[BlockTransformer] = skVarInt
	s[VillagerFood] = skVarInt
	s[StoredEnchantments] = skEnchants
	s[Dye] = skDyeColor
	s[DyedColor] = skI32
	s[MapId] = skVarInt
	s[MapDecorations] = skNBT
	s[MapPostProcessing] = skVarInt
	s[ChargedProjectiles] = list(skTemplate)
	s[BundleContents] = list(skTemplate)
	s[PotionContents] = seq(opt(skVarInt), opt(skI32), list(skMobEffect), opt(skString))
	s[PotionDurationScale] = skF32
	s[SuspiciousStewEffects] = list(seq(skVarInt, skVarInt))
	s[WritableBookContent] = listMax(seq(skString, opt(skString)), 100)
	s[WrittenBookContent] = seq(skString, opt(skString), skString, skVarInt, listMax(seq(skText, opt(skText)), 100), skBool)
	s[Trim] = seq(holder(skTrimMat), holder(skTrimPat))
	s[DebugStickState] = skNBT
	s[EntityData] = seq(skVarInt, skNBT)
	s[BucketEntityData] = skNBT
	s[BlockEntityData] = seq(skVarInt, skNBT)
	s[Instrument] = holder(skInstrument)
	s[ProvidesTrimMaterial] = holder(skTrimMat)
	s[OminousBottleAmplifier] = skVarInt
	s[JukeboxPlayable] = holder(skJukeboxSong)
	s[ProvidesBannerPatterns] = skHolderSet
	s[Recipes] = skNBT
	s[LodestoneTracker] = seq(opt(seq(skID, skI64)), skBool)
	s[FireworkExplosion] = skFirework
	s[Fireworks] = seq(skVarInt, listMax(skFirework, 256))
	s[Profile] = skProfile
	s[NoteBlockSound] = skID
	s[BannerPatterns] = list(seq(holder(skBannerPat), skDyeColor))
	s[BaseColor] = skDyeColor
	s[PotDecorations] = repeat(4, opt(skTemplate))
	s[Container] = listMax(opt(skTemplate), 256)
	s[BlockState] = list(seq(skString, skString))
	s[Bees] = list(seq(skVarInt, skNBT, skVarInt, skVarInt))
	s[SulfurCubeContent] = skTemplate
	s[Lock] = skNBT
	s[ContainerLoot] = skNBT
	s[BreakSound] = skSound
	s[Compostable] = skResolveInt
	s[CookingFuel] = seq(skResolveInt, skResolveF32)
	s[BrewingFuel] = seq(skResolveInt, skResolveF32)
	s[MobVisibility] = seq(skHolderSet, skF32)
	for _, t := range []Type{VillagerVariant, WolfVariant, WolfSoundVariant, FoxVariant, SalmonSize, ParrotVariant,
		TropicalFishPattern, MooshroomVariant, RabbitVariant, PigVariant, PigSoundVariant, CowVariant, CowSoundVariant,
		ChickenSoundVariant, ZombieNautilusVariant, FrogVariant, HorseVariant, LlamaVariant, AxolotlVariant,
		CatVariant, CatSoundVariant, ProvidesPotteryPattern} {
		s[t] = skVarInt
	}
	for _, t := range []Type{WolfCollar, TropicalFishBaseColor, TropicalFishPatternColor, CatCollar, SheepColor, ShulkerColor, CushionColor} {
		s[t] = skDyeColor
	}
	s[ChickenVariant] = skVarInt
	s[PaintingVariant] = holder(skPainting)
	sign := seq(repeat(4, skText), opt(repeat(4, skText)), skDyeColor, skBool)
	s[SignTextFront] = sign
	s[SignTextBack] = sign
	s[Waxed] = skNone
}

func profileSkipper() skipper {
	props := list(seq(skString, skString, opt(skString)))
	return seq(either(
		seq(fixed(16), skString, props),
		seq(opt(skString), opt(fixed(16)), props),
	), opt(skID), opt(skID), opt(skID), opt(skBool))
}

func typeOf(r *proto.Reader) (Type, error) {
	v, err := r.VarInt()
	if err != nil {
		return 0, err
	}
	if v < 0 || v >= int32(NumTypes) {
		return 0, fmt.Errorf("%w: component type %d", ErrMalformed, v)
	}
	return Type(v), nil
}

func skipComponent(r *proto.Reader, d int) error {
	t, err := typeOf(r)
	if err != nil {
		return err
	}
	return skippers[t](r, d)
}

// SkipValue consumes one encoded value of type t.
func SkipValue(t Type, r *proto.Reader) error {
	if t < 0 || t >= NumTypes {
		return ErrMalformed
	}
	return skippers[t](r, 0)
}

// SkipHolderSet consumes one encoded holder set.
func SkipHolderSet(r *proto.Reader) error { return skHolderSet(r, 0) }
