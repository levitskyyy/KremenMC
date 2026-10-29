package kremenmc.probe.spawn;

import com.mojang.authlib.GameProfile;
import java.lang.reflect.Field;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.*;
import net.minecraft.core.BlockPos;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.core.registries.Registries;
import net.minecraft.resources.Identifier;
import net.minecraft.server.level.ClientInformation;
import net.minecraft.server.level.ServerLevel;
import net.minecraft.server.level.ServerPlayer;
import net.minecraft.util.RandomSource;
import net.minecraft.world.attribute.EnvironmentAttributes;
import net.minecraft.world.entity.Entity;
import net.minecraft.world.entity.EntitySpawnReason;
import net.minecraft.world.entity.EntityType;
import net.minecraft.world.entity.Mob;
import net.minecraft.world.entity.MobCategory;
import net.minecraft.world.entity.SpawnPlacements;
import net.minecraft.world.level.ChunkPos;
import net.minecraft.world.level.Level;
import net.minecraft.world.level.LightLayer;
import net.minecraft.world.level.LocalMobCapCalculator;
import net.minecraft.world.level.NaturalSpawner;
import net.minecraft.world.level.block.Block;
import net.minecraft.world.level.chunk.LevelChunk;
import net.minecraft.world.level.levelgen.Heightmap;
import net.minecraft.world.level.levelgen.LegacyRandomSource;
import net.minecraft.world.level.levelgen.WorldgenRandom;
import net.minecraft.world.phys.AABB;

// Scene builds a controlled world from a script, dumps the world state the
// spawn engine reads, then runs spawn operations against the reference with
// recorded random sources. See internal/entity/spawn/testdata/scenes.
public final class Scene {
    static int x0, y0, z0, x1, y1, z1;
    static final List<double[]> players = new ArrayList<>();
    static final List<ServerPlayer> fake = new ArrayList<>();
    static BlockPos worldSpawn = new BlockPos(0, 0, 0);
    static Integer seaOverride;

    // accessor proxies a ServerLevelAccessor onto the real level, overriding
    // the sea level so water rules can be probed in the flat test world.
    static net.minecraft.world.level.ServerLevelAccessor accessor() {
        ServerLevel level = Env.level;
        if (seaOverride == null) return level;
        java.lang.reflect.InvocationHandler h = (proxy, m, a) -> {
            if (m.getName().equals("getSeaLevel") && m.getParameterCount() == 0) return seaOverride;
            if (m.isDefault()) return java.lang.reflect.InvocationHandler.invokeDefault(proxy, m, a);
            try {
                return m.invoke(level, a);
            } catch (java.lang.reflect.InvocationTargetException e) {
                throw e.getCause();
            }
        };
        return (net.minecraft.world.level.ServerLevelAccessor) java.lang.reflect.Proxy.newProxyInstance(Scene.class.getClassLoader(),
            new Class<?>[] {net.minecraft.world.level.ServerLevelAccessor.class}, h);
    }

    public static void main(String[] args) throws Exception {
        List<String> lines = Files.readAllLines(Path.of(args[0]));
        Env.boot();
        var o = Env.out;
        Env.run(() -> {
            Env.command("gamerule spawn_mobs false");
            Env.command("gamerule advance_time false");
            Env.command("gamerule advance_weather false");
            Env.command("gamerule random_tick_speed 0");
            Env.command("gamerule max_block_modifications 10000000");
        });
        List<String> ops = new ArrayList<>();
        for (String raw : lines) {
            String line = raw.strip();
            if (line.isEmpty() || line.startsWith("#")) continue;
            String[] f = line.split("\\s+");
            switch (f[0]) {
                case "area" -> {
                    x0 = Integer.parseInt(f[1]); y0 = Integer.parseInt(f[2]); z0 = Integer.parseInt(f[3]);
                    x1 = Integer.parseInt(f[4]); y1 = Integer.parseInt(f[5]); z1 = Integer.parseInt(f[6]);
                    Env.run(() -> {
                        for (int cx = x0 >> 4; cx <= x1 >> 4; cx++)
                            for (int cz = z0 >> 4; cz <= z1 >> 4; cz++) Env.level.getChunkSource().addTicketWithRadius(net.minecraft.server.level.TicketType.DRAGON, new ChunkPos(cx, cz), 2);
                    });
                    Env.waitTicks(40);
                }
                case "setup" -> {
                    String cmd = line.substring(5).strip();
                    Env.run(() -> Env.command(cmd));
                }
                case "wait" -> Env.waitTicks(Integer.parseInt(f[1]));
                case "sealevel" -> seaOverride = Integer.parseInt(f[1]);
                case "worldspawn" -> worldSpawn = new BlockPos(Integer.parseInt(f[1]), Integer.parseInt(f[2]), Integer.parseInt(f[3]));
                case "player" -> players.add(new double[] {Double.parseDouble(f[1]), Double.parseDouble(f[2]), Double.parseDouble(f[3])});
                default -> ops.add(line);
            }
        }
        Env.waitTicks(20);
        Env.run(() -> Env.server.tickRateManager().setFrozen(true));
        Env.waitTicks(2);
        for (String fl : Env.failures) o.println("warn " + fl);
        Env.run(() -> {
            addPlayers();
            dump(o);
            removePlayers();
        });
        for (String op : ops) {
            String res = Env.call(() -> runOp(op));
            o.print(res);
        }
        Env.exit();
    }

    static Field randomField() throws Exception {
        Field f = Level.class.getDeclaredField("random");
        f.setAccessible(true);
        return f;
    }

    @SuppressWarnings("unchecked")
    static void addPlayers() throws Exception {
        ServerLevel level = Env.level;
        level.setRespawnData(net.minecraft.world.level.storage.LevelData.RespawnData.of(Level.OVERWORLD, worldSpawn, 0, 0));
        Field pf = ServerLevel.class.getDeclaredField("players");
        pf.setAccessible(true);
        var list = (List<ServerPlayer>) pf.get(level);
        int i = 0;
        for (double[] p : players) {
            ServerPlayer sp = new ServerPlayer(Env.server, level, new GameProfile(new UUID(0x5eed, i), "probe" + i), ClientInformation.createDefault());
            sp.snapTo(p[0], p[1], p[2], 0, 0);
            list.add(sp);
            fake.add(sp);
            i++;
        }
    }

    @SuppressWarnings("unchecked")
    static void removePlayers() throws Exception {
        Field pf = ServerLevel.class.getDeclaredField("players");
        pf.setAccessible(true);
        var list = (List<ServerPlayer>) pf.get(Env.level);
        list.removeAll(fake);
        fake.clear();
    }

    static String rle(java.util.function.IntUnaryOperator at, int n) {
        StringBuilder sb = new StringBuilder();
        int prev = 0, cnt = 0;
        for (int i = 0; i < n; i++) {
            int v = at.applyAsInt(i);
            if (cnt > 0 && v == prev) {
                cnt++;
                continue;
            }
            if (cnt > 0) sb.append(' ').append(prev).append('*').append(cnt);
            prev = v;
            cnt = 1;
        }
        if (cnt > 0) sb.append(' ').append(prev).append('*').append(cnt);
        return sb.toString();
    }

    static void dump(java.io.PrintStream o) {
        ServerLevel level = Env.level;
        int nx = x1 - x0 + 1, ny = y1 - y0 + 1, nz = z1 - z0 + 1, n = nx * ny * nz;
        o.println("dump " + x0 + " " + y0 + " " + z0 + " " + x1 + " " + y1 + " " + z1);
        java.util.function.IntFunction<BlockPos> pos = i -> new BlockPos(x0 + i % nx, y0 + i / (nx * nz), z0 + (i / nx) % nz);
        Map<String, Integer> pal = new LinkedHashMap<>();
        int[] ids = new int[n];
        for (int i = 0; i < n; i++) {
            var s = level.getBlockState(pos.apply(i));
            ids[i] = pal.computeIfAbsent(BlockTable.stateString(s), k -> pal.size());
        }
        for (var e : pal.entrySet()) o.println("palette " + e.getValue() + " " + e.getKey());
        o.println("blocks" + rle(i -> ids[i], n));
        o.println("sky" + rle(i -> level.getBrightness(LightLayer.SKY, pos.apply(i)), n));
        o.println("blocklight" + rle(i -> level.getBrightness(LightLayer.BLOCK, pos.apply(i)), n));
        o.println("maxlocal" + rle(i -> level.getMaxLocalRawBrightness(pos.apply(i)), n));
        Map<String, Integer> bpal = new LinkedHashMap<>();
        int[] bids = new int[n];
        for (int i = 0; i < n; i++) bids[i] = bpal.computeIfAbsent(level.getBiome(pos.apply(i)).getRegisteredName(), k -> bpal.size());
        for (var e : bpal.entrySet()) o.println("biomepalette " + e.getValue() + " " + e.getKey());
        o.println("biomes" + rle(i -> bids[i], n));
        for (Heightmap.Types t : new Heightmap.Types[] {Heightmap.Types.WORLD_SURFACE, Heightmap.Types.MOTION_BLOCKING, Heightmap.Types.MOTION_BLOCKING_NO_LEAVES, Heightmap.Types.OCEAN_FLOOR}) {
            o.println("heightmap " + t.getSerializedName() + rle(i -> level.getHeight(t, x0 + i % nx, z0 + i / nx), nx * nz));
        }
        StringBuilder tk = new StringBuilder("ticking");
        for (int cz = z0 >> 4; cz <= z1 >> 4; cz++)
            for (int cx = x0 >> 4; cx <= x1 >> 4; cx++) tk.append(' ').append(level.canSpawnEntitiesInChunk(new ChunkPos(cx, cz)) ? 1 : 0);
        o.println(tk);
        BlockPos c = new BlockPos((x0 + x1) / 2, (y0 + y1) / 2, (z0 + z1) / 2);
        var ea = level.environmentAttributes();
        var rd = level.getRespawnData();
        o.println("env skyDarken=" + level.getSkyDarken() + " thundering=" + level.isThundering() + " raining=" + level.isRaining()
            + " difficulty=" + level.getDifficulty().getSerializedName() + " minY=" + level.getMinY() + " height=" + level.getHeight() + " sea=" + (seaOverride != null ? seaOverride : level.getSeaLevel())
            + " seed=" + level.getSeed() + " moonBrightness=" + Rec.f(level.getMoonBrightness(c))
            + " slimeChance=" + Rec.f(ea.getValue(EnvironmentAttributes.SURFACE_SLIME_SPAWN_CHANCE, c))
            + " worldgenProb=" + Rec.f(ea.getValue(EnvironmentAttributes.CREATURE_WORLD_GEN_SPAWN_PROBABILITY, c))
            + " moonPhase=" + ea.getValue(EnvironmentAttributes.MOON_PHASE, c)
            + " spawn=" + rd.pos().getX() + "," + rd.pos().getY() + "," + rd.pos().getZ()
            + " dayTime=" + level.getDefaultClockTime()
            + " effDifficulty=" + Rec.f(level.getCurrentDifficultyAt(c).getEffectiveDifficulty())
            + " special=" + Rec.f(level.getCurrentDifficultyAt(c).getSpecialMultiplier()));
        for (double[] p : players) o.println("player " + Rec.d(p[0]) + " " + Rec.d(p[1]) + " " + Rec.d(p[2]));
        for (Entity e : level.getEntities((Entity) null, new AABB(x0, y0, z0, x1 + 1, y1 + 1, z1 + 1), e -> true)) {
            o.println("entity " + EntityType.getKey(e.getType()) + " " + Rec.d(e.getX()) + " " + Rec.d(e.getY()) + " " + Rec.d(e.getZ())
                + " persistent=" + (e instanceof Mob m && (m.isPersistenceRequired() || m.requiresCustomPersistence())) + " blocksBuilding=" + e.blocksBuilding);
        }
        o.println("enddump");
    }

    static String runOp(String op) throws Exception {
        String[] f = op.split("\\s+");
        ServerLevel level = Env.level;
        List<String> log = new ArrayList<>();
        List<Entity> spawned = new ArrayList<>();
        Field rf = randomField();
        RandomSource saved = level.getRandom();
        long seed = Long.parseLong(f[f.length - 1]);
        Rec lr = new Rec(new LegacyRandomSource(seed), "l", log);
        addPlayers();
        rf.set(level, lr);
        StringBuilder out = new StringBuilder("op " + op + "\n");
        try {
            switch (f[0]) {
                case "rules" -> {
                    EntityType<?> t = type(f[1]);
                    BlockPos p = new BlockPos(Integer.parseInt(f[2]), Integer.parseInt(f[3]), Integer.parseInt(f[4]));
                    Rec r = new Rec(new LegacyRandomSource(seed ^ 0x5DEECE66DL), "p", log);
                    var acc = accessor();
                    boolean ok = SpawnPlacements.isSpawnPositionOk(t, acc, p);
                    log.add("m placement " + ok);
                    EntitySpawnReason reason = f.length > 6 ? EntitySpawnReason.valueOf(f[5]) : EntitySpawnReason.NATURAL;
                    boolean rules = SpawnPlacements.checkSpawnRules(t, acc, reason, p, r);
                    log.add("m rules " + rules);
                }
                case "mobcheck" -> {
                    EntityType<?> t = type(f[1]);
                    BlockPos p = new BlockPos(Integer.parseInt(f[2]), Integer.parseInt(f[3]), Integer.parseInt(f[4]));
                    log.add("m light magic=" + Rec.f(level.getLightLevelDependentMagicValue(p)) + " cost=" + Rec.f(level.getPathfindingCostFromLightLevels(p))
                        + " maxlocal=" + level.getMaxLocalRawBrightness(p) + " effsky=" + level.getEffectiveSkyBrightness(p) + " raw0=" + level.getRawBrightness(p, 0));
                    var e = t.create(level, EntitySpawnReason.NATURAL);
                    if (e instanceof Mob m) {
                        m.snapTo(p.getX() + 0.5, p.getY(), p.getZ() + 0.5, 0, 0);
                        if (m instanceof net.minecraft.world.entity.PathfinderMob pm) log.add("m walk " + Rec.f(pm.getWalkTargetValue(p, level)));
                        log.add("m mobrules " + m.checkSpawnRules(level, EntitySpawnReason.NATURAL));
                        log.add("m obstruction " + m.checkSpawnObstruction(level));
                    }
                    if (e != null) e.discard();
                }
                case "natural" -> {
                    MobCategory cat = category(f[1]);
                    BlockPos p = new BlockPos(Integer.parseInt(f[2]), Integer.parseInt(f[3]), Integer.parseInt(f[4]));
                    LevelChunk ch = level.getChunk(p.getX() >> 4, p.getZ() >> 4);
                    NaturalSpawner.spawnCategoryForPosition(cat, level, ch, p, (t, l, bp, c) -> {
                        log.add("m pred " + EntityType.getKey(t) + " " + bp.getX() + " " + bp.getY() + " " + bp.getZ());
                        return true;
                    }, (m, c) -> {
                        spawned.add(m);
                        log.add("m spawned " + spawnedLine(m));
                    });
                }
                case "chunk" -> {
                    MobCategory cat = category(f[1]);
                    LevelChunk ch = level.getChunk(Integer.parseInt(f[2]), Integer.parseInt(f[3]));
                    NaturalSpawner.spawnCategoryForChunk(cat, level, ch, (t, l, bp, c) -> {
                        log.add("m pred " + EntityType.getKey(t) + " " + bp.getX() + " " + bp.getY() + " " + bp.getZ());
                        return true;
                    }, (m, c) -> {
                        spawned.add(m);
                        log.add("m spawned " + spawnedLine(m));
                    });
                }
                case "tick" -> {
                    // tick cx cz chunkCount friendly enemies seed
                    LevelChunk ch = level.getChunk(Integer.parseInt(f[1]), Integer.parseInt(f[2]));
                    int count = Integer.parseInt(f[3]);
                    var calc = new LocalMobCapCalculator(level.getChunkSource().chunkMap);
                    var state = NaturalSpawner.createState(count, level, (k, cons) -> {
                        var c = level.getChunkSource().getChunkNow(ChunkPos.getX(k), ChunkPos.getZ(k));
                        if (c != null) cons.accept(c);
                    }, calc);
                    var cats = NaturalSpawner.getFilteredSpawningCategories(state, f[4].equals("1"), f[5].equals("1"));
                    StringBuilder sb = new StringBuilder("m categories");
                    for (var c : cats) sb.append(' ').append(c.getName());
                    log.add(sb.toString());
                    Runnable det = detector(new HashSet<>(), log, spawned);
                    lr.before = det;
                    NaturalSpawner.spawnForChunk(level, ch, state, cats);
                    det.run();
                }
                case "potential" -> {
                    // potential category cx cz seed: spawnCategoryForChunk gated by a real SpawnState
                    MobCategory cat = category(f[1]);
                    LevelChunk ch = level.getChunk(Integer.parseInt(f[2]), Integer.parseInt(f[3]));
                    var calc = new LocalMobCapCalculator(level.getChunkSource().chunkMap);
                    var state = NaturalSpawner.createState(1, level, (k, cons) -> {
                        var c = level.getChunkSource().getChunkNow(ChunkPos.getX(k), ChunkPos.getZ(k));
                        if (c != null) cons.accept(c);
                    }, calc);
                    var cs = NaturalSpawner.SpawnState.class.getDeclaredMethod("canSpawn", EntityType.class, Level.class, BlockPos.class, net.minecraft.world.level.chunk.ChunkAccess.class);
                    var as = NaturalSpawner.SpawnState.class.getDeclaredMethod("afterSpawn", Mob.class, net.minecraft.world.level.chunk.ChunkAccess.class);
                    cs.setAccessible(true);
                    as.setAccessible(true);
                    NaturalSpawner.spawnCategoryForChunk(cat, level, ch, (t, l, bp, c) -> {
                        try {
                            boolean ok = (boolean) cs.invoke(state, t, l, bp, c);
                            log.add("m canspawn " + EntityType.getKey(t) + " " + bp.getX() + " " + bp.getY() + " " + bp.getZ() + " " + ok);
                            return ok;
                        } catch (ReflectiveOperationException e) {
                            throw new RuntimeException(e);
                        }
                    }, (m, c) -> {
                        try {
                            as.invoke(state, m, c);
                        } catch (ReflectiveOperationException e) {
                            throw new RuntimeException(e);
                        }
                        spawned.add(m);
                        log.add("m spawned " + spawnedLine(m));
                    });
                }
                case "gen" -> {
                    int cx = Integer.parseInt(f[1]), cz = Integer.parseInt(f[2]);
                    Rec w = new Rec(new LegacyRandomSource(seed ^ 0x9E3779B97F4A7C15L), "w", log);
                    Runnable det = detector(new HashSet<>(), log, spawned);
                    lr.before = det;
                    w.before = det;
                    ChunkPos cp = new ChunkPos(cx, cz);
                    Env.command("gamerule spawn_mobs " + (f.length > 4 ? f[3] : "true"));
                    NaturalSpawner.spawnMobsForChunkGeneration(level, cp.getMiddleBlockPosition(level.getSeaLevel()), cp, w);
                    Env.command("gamerule spawn_mobs false");
                    det.run();
                }
                case "despawn" -> {
                    // despawn type x y z noActionTime flags seed
                    EntityType<?> t = type(f[1]);
                    var e = t.create(level, EntitySpawnReason.NATURAL);
                    if (e instanceof Mob m) {
                        m.snapTo(Double.parseDouble(f[2]), Double.parseDouble(f[3]), Double.parseDouble(f[4]), 0, 0);
                        m.setNoActionTime(Integer.parseInt(f[5]));
                        if (f[6].contains("named")) m.setCustomName(net.minecraft.network.chat.Component.literal("n"));
                        if (f[6].contains("persistent")) m.setPersistenceRequired();
                        Field ef = Entity.class.getDeclaredField("random");
                        ef.setAccessible(true);
                        ef.set(m, new Rec(new LegacyRandomSource(seed), "e", log));
                        m.checkDespawn();
                        log.add("m removed " + m.isRemoved() + " noaction " + m.getNoActionTime());
                    }
                    if (e != null) e.discard();
                }
                case "spawner" -> {
                    // spawner type x y z ticks delay min max count maxNearby range playerRange seed
                    EntityType<?> t = type(f[1]);
                    BlockPos p = new BlockPos(Integer.parseInt(f[2]), Integer.parseInt(f[3]), Integer.parseInt(f[4]));
                    level.setBlock(p, net.minecraft.world.level.block.Blocks.SPAWNER.defaultBlockState(), 2);
                    var be = (net.minecraft.world.level.block.entity.SpawnerBlockEntity) level.getBlockEntity(p);
                    be.setEntityId(t, lr);
                    var bs = be.getSpawner();
                    String[] names = {"spawnDelay", "minSpawnDelay", "maxSpawnDelay", "spawnCount", "maxNearbyEntities", "spawnRange", "requiredPlayerRange"};
                    for (int i = 0; i < names.length; i++) {
                        Field bf = net.minecraft.world.level.BaseSpawner.class.getDeclaredField(names[i]);
                        bf.setAccessible(true);
                        bf.setInt(bs, Integer.parseInt(f[6 + i]));
                    }
                    Field df = net.minecraft.world.level.BaseSpawner.class.getDeclaredField("spawnDelay");
                    df.setAccessible(true);
                    int ticks = Integer.parseInt(f[5]);
                    Runnable det = detector(new HashSet<>(), log, spawned);
                    lr.before = det;
                    for (int i = 0; i < ticks; i++) {
                        bs.serverTick(level, p);
                        det.run();
                        log.add("m tick " + i + " delay=" + df.getInt(bs));
                    }
                    level.setBlock(p, net.minecraft.world.level.block.Blocks.AIR.defaultBlockState(), 2);
                }
                default -> throw new IllegalArgumentException("unknown op " + op);
            }
        } finally {
            rf.set(level, saved);
            for (Entity e : spawned) {
                for (Entity p : e.getPassengers()) p.discard();
                e.discard();
            }
            removePlayers();
        }
        for (String l : log) out.append(l).append('\n');
        out.append("endop\n");
        return out.toString();
    }

    // detector logs mobs that appeared in the level since the last call, so
    // spawn markers land between the draws that surround the spawn.
    static Runnable detector(Set<Entity> seen, List<String> log, List<Entity> spawned) {
        level0().getAllEntities().forEach(seen::add);
        return () -> {
            for (Entity e : level0().getAllEntities()) {
                if (seen.add(e) && e instanceof Mob m && !m.isPassenger()) {
                    spawned.add(m);
                    log.add("m spawned " + spawnedLine(m));
                }
            }
        };
    }

    static ServerLevel level0() {
        return Env.level;
    }

    static String spawnedLine(Mob m) {
        var d = Env.level.getCurrentDifficultyAt(m.blockPosition());
        StringBuilder sb = new StringBuilder(EntityType.getKey(m.getType()) + " " + Rec.d(m.getX()) + " " + Rec.d(m.getY()) + " " + Rec.d(m.getZ())
            + " " + Rec.f(m.getYRot()) + " eff=" + Rec.f(d.getEffectiveDifficulty()) + " special=" + Rec.f(d.getSpecialMultiplier()));
        StringBuilder g = new StringBuilder();
        for (int k = 0; k <= 16; k++) if (m.isMaxGroupSizeReached(k)) g.append(k).append(',');
        sb.append(" group=").append(g.length() == 0 ? "none" : g.substring(0, g.length() - 1)).append(" cluster=").append(m.getMaxSpawnClusterSize());
        for (Entity p : m.getPassengers()) sb.append(" passenger=").append(EntityType.getKey(p.getType()));
        if (m.getVehicle() != null) sb.append(" vehicle=").append(EntityType.getKey(m.getVehicle().getType()));
        return sb.toString();
    }

    static EntityType<?> type(String key) {
        return BuiltInRegistries.ENTITY_TYPE.getValue(Identifier.parse(key));
    }

    static MobCategory category(String name) {
        for (MobCategory c : MobCategory.values()) if (c.getName().equals(name)) return c;
        throw new IllegalArgumentException(name);
    }
}
