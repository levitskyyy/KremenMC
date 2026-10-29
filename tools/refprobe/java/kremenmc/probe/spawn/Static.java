package kremenmc.probe.spawn;

import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.world.entity.EntityType;
import net.minecraft.world.entity.MobCategory;
import net.minecraft.world.entity.SpawnPlacementType;
import net.minecraft.world.entity.SpawnPlacementTypes;
import net.minecraft.world.entity.SpawnPlacements;
import net.minecraft.world.level.NaturalSpawner;
import net.minecraft.world.level.gamerules.GameRules;

// Static prints spawn data that needs no world: mob categories, spawn
// placement table for every entity type, NaturalSpawner constants, game rules.
public final class Static {
    public static void main(String[] args) throws Exception {
        Env.init();
        var o = Env.out;
        for (MobCategory c : MobCategory.values()) {
            o.println("category " + c.ordinal() + " " + c.getName() + " max=" + c.getMaxInstancesPerChunk() + " friendly=" + c.isFriendly()
                + " persistent=" + c.isPersistent() + " despawn=" + c.getDespawnDistance() + " nodespawn=" + c.getNoDespawnDistance()
                + " abbr=" + c.getDebugAbbreviation());
        }
        for (String f : new String[] {"MIN_SPAWN_DISTANCE", "SPAWN_DISTANCE_CHUNK", "SPAWN_DISTANCE_BLOCK", "INSCRIBED_SQUARE_SPAWN_DISTANCE_CHUNK", "MAGIC_NUMBER"}) {
            var fl = NaturalSpawner.class.getDeclaredField(f);
            fl.setAccessible(true);
            o.println("const " + f + " " + fl.get(null));
        }
        var sc = NaturalSpawner.class.getDeclaredField("SPAWNING_CATEGORIES");
        sc.setAccessible(true);
        StringBuilder sb = new StringBuilder("spawning_categories");
        for (MobCategory c : (MobCategory[]) sc.get(null)) sb.append(' ').append(c.getName());
        o.println(sb);
        var dataField = SpawnPlacements.class.getDeclaredField("DATA_BY_TYPE");
        dataField.setAccessible(true);
        var data = (java.util.Map<?, ?>) dataField.get(null);
        for (EntityType<?> t : BuiltInRegistries.ENTITY_TYPE) {
            o.println("entity " + BuiltInRegistries.ENTITY_TYPE.getId(t) + " " + EntityType.getKey(t) + " cat=" + t.getCategory().getName()
                + " summon=" + t.canSummon() + " far=" + t.canSpawnFarFromPlayer() + " fireImmune=" + t.fireImmune()
                + " w=" + Rec.f(t.getWidth()) + " h=" + Rec.f(t.getHeight()) + " peaceful=" + t.isAllowedInPeaceful()
                + " registered=" + data.containsKey(t) + " placement=" + placementName(SpawnPlacements.getPlacementType(t))
                + " heightmap=" + SpawnPlacements.getHeightmapType(t).getSerializedName() + " width=" + t.getWidth() + " height=" + t.getHeight());
        }
        for (var f : GameRules.class.getFields()) {
            if (f.getType() == net.minecraft.world.level.gamerules.GameRule.class) {
                var r = (net.minecraft.world.level.gamerules.GameRule<?>) f.get(null);
                String n = f.getName();
                if (n.contains("SPAWN") || n.contains("MOB") || n.contains("RAID") || n.contains("DAYLIGHT") || n.contains("TIME")) {
                    o.println("gamerule " + n + " " + r.id() + " default=" + r.defaultValue());
                }
            }
        }
        long[] times = {0, 24000, 72000, 100000, 500000, 1512000, 3000000};
        long[] inhabited = {0, 1000, 360000, 3600000, 9000000};
        float[] moons = {0f, 0.25f, 0.5f, 0.75f, 1f};
        for (net.minecraft.world.Difficulty d : net.minecraft.world.Difficulty.values())
            for (long t : times)
                for (long ih : inhabited)
                    for (float mb : moons) {
                        var di = new net.minecraft.world.DifficultyInstance(d, t, ih, mb);
                        o.println("difficulty " + d.getId() + " " + t + " " + ih + " " + Rec.f(mb) + " " + Rec.f(di.getEffectiveDifficulty()) + " " + Rec.f(di.getSpecialMultiplier()));
                    }
        for (String f : new String[] {"DEFAULT_SPAWN_DELAY", "DEFAULT_MIN_SPAWN_DELAY", "DEFAULT_MAX_SPAWN_DELAY", "DEFAULT_SPAWN_COUNT", "DEFAULT_MAX_NEARBY_ENTITIES", "DEFAULT_REQUIRED_PLAYER_RANGE", "DEFAULT_SPAWN_RANGE"}) {
            var fl = net.minecraft.world.level.BaseSpawner.class.getDeclaredField(f);
            fl.setAccessible(true);
            o.println("spawner " + f + " " + fl.get(null));
        }
        o.println("halloween " + net.minecraft.util.SpecialDates.HALLOWEEN + " today=" + net.minecraft.util.SpecialDates.dayNow()
            + " isHalloween=" + net.minecraft.util.SpecialDates.isHalloween());
        Env.out.flush();
    }

    static String placementName(SpawnPlacementType p) {
        if (p == SpawnPlacementTypes.NO_RESTRICTIONS) return "no_restrictions";
        if (p == SpawnPlacementTypes.IN_WATER) return "in_water";
        if (p == SpawnPlacementTypes.IN_LAVA) return "in_lava";
        if (p == SpawnPlacementTypes.ON_GROUND) return "on_ground";
        return "unknown:" + p;
    }
}
