package kremenmc.probe.temple;

import java.io.PrintStream;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.Optional;
import java.util.concurrent.CompletableFuture;
import net.minecraft.SharedConstants;
import net.minecraft.commands.arguments.blocks.BlockStateParser;
import net.minecraft.core.BlockPos;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.core.registries.Registries;
import net.minecraft.gametest.framework.GameTestServer;
import net.minecraft.nbt.CompoundTag;
import net.minecraft.nbt.TagParser;
import net.minecraft.server.MinecraftServer;
import net.minecraft.server.level.ServerLevel;
import net.minecraft.server.packs.repository.ServerPacksSource;
import net.minecraft.world.level.ChunkPos;
import net.minecraft.world.level.block.state.BlockState;
import net.minecraft.world.level.block.state.properties.Property;
import net.minecraft.world.level.chunk.LevelChunk;
import net.minecraft.world.level.levelgen.WorldgenRandom;
import net.minecraft.world.level.levelgen.XoroshiroRandomSource;
import net.minecraft.world.level.levelgen.structure.BoundingBox;
import net.minecraft.world.level.levelgen.structure.StructureStart;
import net.minecraft.world.level.levelgen.structure.pieces.StructurePieceSerializationContext;
import net.minecraft.world.level.storage.LevelStorageSource;
import net.minecraft.world.phys.AABB;

// TScene is rportal.RPScene with heightmap reads (getHeight,
// getHeightmapPos) answered from the scene's live blocks: the first block
// from the top that the heightmap type's predicate accepts (the GameTest
// world's heightmaps do not see the raw scene writes).
// RPScene is MineScene (kremenmc.probe.legacy) with setBlockAndUpdate also
// written raw and neighbour updates / scheduled ticks dropped.
// MineScene loads an explicit block scene into the flat GameTest world,
// loads a saved structure start (StructureStart.loadStaticStart) and runs
// its placeInChunk for a list of chunks in order, each with the random
// WorldgenRandom(xoroshiro).setDecorationSeed(seed, minX, minZ) +
// setFeatureSeed(deco, index, step). Prints every block of the scene that
// changed, block entities, entities, and per chunk the random's next long.
// LOG=1 logs every draw with its reference call path.
//
// Input file:
//   SEED <worldSeed> <index> <step>
//   REGION x0 y0 z0 x1 y1 z1
//   PAL <n>, then n state strings
//   DATA <idx*count ...> (order y, z, x)
//   START <snbt start compound>
//   CHUNKS cx,cz ...
public final class TScene {
    // A real WorldGenRegion.setBlock returns true for an unchanged state
    // (oruin.RegionSet); RAWCHG=1 restores the old false.
    static final boolean RAWCHG = System.getenv("RAWCHG") != null;

    // With several input files each output goes to <input minus .txt>.out.
    // Later scenes see what earlier ones wrote outside their region (giant
    // portal spreads), so scenes sharing an area need separate runs.
    static PrintStream open(String arg, PrintStream def, int n) {
        if (n == 1) return def;
        try { return new PrintStream(new java.io.FileOutputStream(arg.replaceAll("\\.txt$", "") + ".out"), true); } catch (Exception e) { throw new RuntimeException(e); }
    }

    static String state(BlockState s) {
        StringBuilder b = new StringBuilder(BuiltInRegistries.BLOCK.getKey(s.getBlock()).getPath());
        String sep = "[";
        for (Property<?> p : s.getProperties()) {
            b.append(sep).append(p.getName()).append('=').append(s.getValue(p).toString().toLowerCase());
            sep = ",";
        }
        if (!sep.equals("[")) b.append(']');
        return b.toString();
    }

    static boolean rawSet(ServerLevel level, BlockPos p, BlockState s) {
        LevelChunk ch = level.getChunk(p.getX() >> 4, p.getZ() >> 4);
        BlockState old = ch.getSection(ch.getSectionIndex(p.getY())).setBlockState(p.getX() & 15, p.getY() & 15, p.getZ() & 15, s, false);
        if (old.hasBlockEntity()) ch.removeBlockEntity(p);
        if (s.hasBlockEntity() && s.getBlock() instanceof net.minecraft.world.level.block.EntityBlock eb) {
            var be = eb.newBlockEntity(p, s);
            if (be != null) ch.setBlockEntity(be);
        }
        return true;
    }

    static boolean rawSetChanged(ServerLevel level, BlockPos p, BlockState s) {
        LevelChunk ch = level.getChunk(p.getX() >> 4, p.getZ() >> 4);
        if (ch.getSection(ch.getSectionIndex(p.getY())).getBlockState(p.getX() & 15, p.getY() & 15, p.getZ() & 15) == s) return false;
        return rawSet(level, p, s);
    }

    static int TOP;
    static long WSEED;
    static int colHeight(ServerLevel level, net.minecraft.world.level.levelgen.Heightmap.Types t, int x, int z) {
        LevelChunk ch = level.getChunk(x >> 4, z >> 4);
        for (int y = TOP; y >= level.getMinY(); y--) {
            BlockState s = ch.getSection(ch.getSectionIndex(y)).getBlockState(x & 15, y & 15, z & 15);
            if (t.isOpaque().test(s)) return y + 1;
        }
        return level.getMinY();
    }

    public static void main(String[] args) throws Exception {
        PrintStream out0 = new PrintStream(new java.io.FileOutputStream(java.io.FileDescriptor.out), true);
        System.setOut(System.err);
        LogWR.OUT = out0;
        LogWR.LIMIT = System.getenv("PATHLIM") != null ? Integer.parseInt(System.getenv("PATHLIM")) : 3;
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        Path dir = Files.createTempDirectory(Path.of("").toAbsolutePath(), "tsceneworld-");
        var access = LevelStorageSource.createDefault(dir).createAccess("world");
        var packs = ServerPacksSource.createPackRepository(access);
        MinecraftServer server = MinecraftServer.spin(t -> GameTestServer.create(t, access, packs, Optional.of("minecraft:always_pass"), false, 1000000));
        for (int i = 0; i < 1200 && !server.isReady(); i++) Thread.sleep(50);
        CompletableFuture<Void> f = new CompletableFuture<>();
        server.execute(() -> {
          for (String arg : args) {
            final PrintStream out = open(arg, out0, args.length);
            LogWR.OUT = out;
            try {
                var lines = Files.readAllLines(Path.of(arg));
                ServerLevel level = server.overworld();
                long seed = 0;
                int index = 0, step = 0;
                int[] rg = null;
                List<BlockState> pal = new ArrayList<>();
                BlockState[] scene = null;
                CompoundTag start = null;
                List<int[]> chunks = new ArrayList<>();
                for (int li = 0; li < lines.size(); li++) {
                    String l = lines.get(li);
                    String[] a = l.split(" ");
                    switch (a[0]) {
                        case "SEED" -> { seed = Long.parseLong(a[1]); index = Integer.parseInt(a[2]); step = Integer.parseInt(a[3]); }
                        case "REGION" -> { rg = new int[6]; for (int i = 0; i < 6; i++) rg[i] = Integer.parseInt(a[i + 1]); }
                        case "PAL" -> {
                            int n = Integer.parseInt(a[1]);
                            for (int i = 0; i < n; i++) pal.add(BlockStateParser.parseForBlock(level.holderLookup(Registries.BLOCK), lines.get(++li), false).blockState());
                        }
                        case "DATA" -> {
                            int sx = rg[3] - rg[0] + 1, sy = rg[4] - rg[1] + 1, sz = rg[5] - rg[2] + 1;
                            scene = new BlockState[sx * sy * sz];
                            int k = 0;
                            for (int i = 1; i < a.length; i++) {
                                String[] p = a[i].split("\\*");
                                BlockState s = pal.get(Integer.parseInt(p[0]));
                                int c = p.length > 1 ? Integer.parseInt(p[1]) : 1;
                                for (int j = 0; j < c; j++) scene[k++] = s;
                            }
                        }
                        case "START" -> start = TagParser.parseCompoundFully(l.substring(6));
                        case "CHUNKS" -> { for (int i = 1; i < a.length; i++) { String[] p = a[i].split(","); chunks.add(new int[]{Integer.parseInt(p[0]), Integer.parseInt(p[1])}); } }
                        default -> {}
                    }
                }
                int sx = rg[3] - rg[0] + 1, sz = rg[5] - rg[2] + 1;
                TOP = rg[4] + 40;
                WSEED = seed;
                for (int cx = rg[0] >> 4; cx <= rg[3] >> 4; cx++) for (int cz = rg[2] >> 4; cz <= rg[5] >> 4; cz++) level.getChunk(cx, cz);
                AABB ebox = new AABB(rg[0] - 2, rg[1] - 2, rg[2] - 2, rg[3] + 3, rg[4] + 3, rg[5] + 3);
                for (var e : level.getEntities((net.minecraft.world.entity.Entity) null, ebox, e -> true)) e.discard();
                int k = 0;
                for (int y = rg[1]; y <= rg[4]; y++) for (int z = rg[2]; z <= rg[5]; z++) for (int x = rg[0]; x <= rg[3]; x++) {
                    BlockPos p = new BlockPos(x, y, z);
                    level.removeBlockEntity(p);
                    LevelChunk ch = level.getChunk(x >> 4, z >> 4);
                    ch.getSection(ch.getSectionIndex(y)).setBlockState(x & 15, y & 15, z & 15, scene[k++], false);
                }
                StructureStart st = StructureStart.loadStaticStart(StructurePieceSerializationContext.fromLevel(level), start, seed);
                out.println("PIECES " + st.getPieces().size());
                for (int[] c : chunks) {
                    LogWR r = new LogWR(new XoroshiroRandomSource(0));
                    long deco = r.setDecorationSeed(seed, c[0] << 4, c[1] << 4);
                    r.setFeatureSeed(deco, index, step);
                    r.on = System.getenv("LOG") != null;
                    out.println("CHUNK " + c[0] + " " + c[1]);
                    // FULLCLIP=1: one pass clipped to the whole scene region.
                    BoundingBox cb = System.getenv("FULLCLIP") != null ? new BoundingBox(rg[0], level.getMinY(), rg[2], rg[3], level.getMaxY(), rg[5])
                        : new BoundingBox(c[0] << 4, level.getMinY(), c[1] << 4, (c[0] << 4) + 15, level.getMaxY(), (c[1] << 4) + 15);
                    final ServerLevel lvl = level;
                    // BIOME=<id> answers every getBiome with that biome.
                    final Object biome = System.getenv("BIOME") == null ? null : level.registryAccess().lookupOrThrow(Registries.BIOME)
                        .getOrThrow(net.minecraft.resources.ResourceKey.create(Registries.BIOME, net.minecraft.resources.Identifier.parse(System.getenv("BIOME"))));
                    final boolean logw = System.getenv("LOGW") != null, reads = "2".equals(System.getenv("LOGW"));
                    final boolean serverWrites = System.getenv("SERVERWRITES") != null;
                    // level.getRandom() is WorldGenRegion's random: the level
                    // RandomState's factory "minecraft:worldgen_region_random"
                    // at the chunk min corner, y 0 (RegionRand).
                    var nset = level.registryAccess().lookupOrThrow(Registries.NOISE_SETTINGS).getOrThrow(net.minecraft.world.level.levelgen.NoiseGeneratorSettings.OVERWORLD).value();
                    var wrs = net.minecraft.world.level.levelgen.RandomState.create(level.registryAccess().lookupOrThrow(Registries.NOISE), seed, nset);
                    final LogRS regionRand = new LogRS(wrs.getOrCreateRandomFactory(net.minecraft.resources.Identifier.parse("minecraft:worldgen_region_random")).at(c[0] << 4, 0, c[1] << 4));
                    regionRand.on = System.getenv("LOG") != null;
                    // A proxy level: writes go straight into the chunk sections
                    // (no shape updates, no onPlace), as a proto-chunk would;
                    // LOGW logs writes (and reads with LOGW=2) with call paths.
                    net.minecraft.world.level.WorldGenLevel wl = (net.minecraft.world.level.WorldGenLevel) java.lang.reflect.Proxy.newProxyInstance(TScene.class.getClassLoader(),
                        new Class<?>[]{net.minecraft.world.level.WorldGenLevel.class}, (proxy, m, margs) -> {
                            String n = m.getName();
                            Object res;
                            // Every write is a raw section write and neighbour
                            // notifications are dropped, as in a proto chunk.
                            if ((n.equals("setBlock") || n.equals("setBlockAndUpdate")) && !serverWrites) res = (RAWCHG ? rawSetChanged(lvl, (BlockPos) margs[0], (BlockState) margs[1]) : rawSet(lvl, (BlockPos) margs[0], (BlockState) margs[1]));
                            else if (!serverWrites && (n.equals("updateNeighborsAt") || n.equals("blockUpdated") || n.equals("updateNeighbourForOutputSignal") || n.equals("scheduleTick"))) res = null;
                            else if (n.equals("getBiome") && biome != null) res = biome;
                            else if (n.equals("getHeight") && margs != null && margs.length == 3) res = colHeight(lvl, (net.minecraft.world.level.levelgen.Heightmap.Types) margs[0], (Integer) margs[1], (Integer) margs[2]);
                            else if (n.equals("getRandom")) res = regionRand;
                            else if (n.equals("getSeed")) res = System.getenv("SEEDOVR") != null ? Long.parseLong(System.getenv("SEEDOVR")) : WSEED;
                            else if (n.equals("getHeightmapPos")) { BlockPos bp = (BlockPos) margs[1]; res = new BlockPos(bp.getX(), colHeight(lvl, (net.minecraft.world.level.levelgen.Heightmap.Types) margs[0], bp.getX(), bp.getZ()), bp.getZ()); }
                            else res = m.invoke(lvl, margs);
                            boolean hm = n.equals("getHeight") && margs != null && margs.length > 0;
                            if (logw && (n.equals("setBlock") || n.equals("addFreshEntity") || hm || (reads && !n.equals("getMinY") && !n.equals("getMaxY") && !n.equals("getHeight") && !n.equals("isOutsideBuildHeight")))) {
                                StringBuilder b = new StringBuilder("W ").append(n);
                                if (margs != null) for (Object o : margs) {
                                    if (o instanceof BlockPos bp) b.append(' ').append(bp.getX()).append(',').append(bp.getY()).append(',').append(bp.getZ());
                                    else if (o instanceof BlockState bs) b.append(' ').append(state(bs));
                                    else b.append(' ').append(o);
                                }
                                if (res instanceof BlockState bs) b.append(" -> ").append(state(bs));
                                else if (res != null) b.append(" -> ").append(res);
                                b.append(" @").append(LogWR.path());
                                out.println(b);
                            }
                            return res;
                        });
                    if (System.getenv("SPLIT") != null) {
                        // postProcess every piece, print the public state
                        // getters (potential suspicious sand list, roof pos),
                        // then afterPlace, as placeInChunk would.
                        var ps = st.getPieces();
                        for (int i = 0; i < ps.size(); i++) {
                            if (!ps.get(i).getBoundingBox().intersects(cb)) continue;
                            ps.get(i).postProcess(wl, level.structureManager(), level.getChunkSource().getGenerator(), r, cb, new ChunkPos(c[0], c[1]), BlockPos.ZERO);
                            for (String g : new String[]{"getPotentialSuspiciousSandWorldPositions", "getRandomCollapsedRoofPos"}) {
                                try {
                                    Object v = ps.get(i).getClass().getMethod(g).invoke(ps.get(i));
                                    StringBuilder vb = new StringBuilder("STATE ").append(i).append(' ').append(g);
                                    if (v instanceof java.util.List<?> lst && System.getenv("REVLIST") != null) {
                                        java.util.Collections.reverse(lst);
                                        Object again = ps.get(i).getClass().getMethod(g).invoke(ps.get(i));
                                        out.println("REVCHECK same=" + (again == lst) + " class=" + lst.getClass().getName() + " first=" + ((java.util.List<?>) again).get(0));
                                    }
                                    if (v instanceof java.util.List<?> lst) for (Object o : lst) { BlockPos bp = (BlockPos) o; vb.append(' ').append(bp.getX()).append(',').append(bp.getY()).append(',').append(bp.getZ()); }
                                    else if (v instanceof BlockPos bp) vb.append(' ').append(bp.getX()).append(',').append(bp.getY()).append(',').append(bp.getZ());
                                    else vb.append(' ').append(v);
                                    out.println(vb);
                                } catch (NoSuchMethodException e) {
                                } catch (Exception e) { throw new RuntimeException(e); }
                            }
                        }
                        st.getStructure().afterPlace(wl, level.structureManager(), level.getChunkSource().getGenerator(), r, cb, new ChunkPos(c[0], c[1]), new net.minecraft.world.level.levelgen.structure.pieces.PiecesContainer(ps));
                    } else if (System.getenv("PERPIECE") == null) {
                        st.placeInChunk(wl, level.structureManager(), level.getChunkSource().getGenerator(), r, cb, new ChunkPos(c[0], c[1]));
                    } else {
                        // Same loop by hand, to tag draws with the piece index.
                        var ps = st.getPieces();
                        for (int i = 0; i < ps.size(); i++) {
                            if (!ps.get(i).getBoundingBox().intersects(cb)) continue;
                            out.println("PIECE " + i);
                            ps.get(i).postProcess(wl, level.structureManager(), level.getChunkSource().getGenerator(), r, cb, new ChunkPos(c[0], c[1]), BlockPos.ZERO);
                        }
                    }
                    r.on = false;
                    out.println("NEXT " + c[0] + " " + c[1] + " " + r.nextLong());
                }
                StringBuilder sb = new StringBuilder();
                k = 0;
                for (int y = rg[1]; y <= rg[4]; y++) for (int z = rg[2]; z <= rg[5]; z++) for (int x = rg[0]; x <= rg[3]; x++) {
                    BlockPos p = new BlockPos(x, y, z);
                    LevelChunk rc = level.getChunk(x >> 4, z >> 4);
                    // Read the section directly: an all-air section reads as air
                    // through the chunk even where cave_air was written.
                    BlockState s = rc.getSection(rc.getSectionIndex(y)).getBlockState(x & 15, y & 15, z & 15);
                    if (s != scene[k++]) sb.append("B ").append(x).append(' ').append(y).append(' ').append(z).append(' ').append(state(s)).append('\n');
                    var be = level.getBlockEntity(p);
                    if (be != null) sb.append("BE ").append(x).append(' ').append(y).append(' ').append(z).append(' ').append(be.saveWithoutMetadata(level.registryAccess())).append('\n');
                }
                for (var e : level.getEntities((net.minecraft.world.entity.Entity) null, ebox, e -> true)) {
                    var to = net.minecraft.world.level.storage.TagValueOutput.createWithContext(net.minecraft.util.ProblemReporter.DISCARDING, level.registryAccess());
                    e.saveWithoutId(to);
                    CompoundTag et = to.buildResult();
                    sb.append("E ").append(BuiltInRegistries.ENTITY_TYPE.getKey(e.getType())).append(' ').append(e.getX()).append(' ').append(e.getY()).append(' ').append(e.getZ())
                        .append(' ').append(et.get("LootTable")).append(' ').append(et.get("LootTableSeed")).append(' ').append(et.get("Rotation")).append('\n');
                }
                CompoundTag after = st.createTag(StructurePieceSerializationContext.fromLevel(level), new ChunkPos(start.getIntOr("ChunkX", 0), start.getIntOr("ChunkZ", 0)));
                sb.append("AFTER ").append(after).append('\n');
                out.print(sb);
                out.flush();
                if (out != out0) out.close();
            } catch (Throwable t) {
                t.printStackTrace();
                f.completeExceptionally(t);
            }
          }
          f.complete(null);
        });
        try {
            f.get();
        } finally {
            out0.flush();
            Runtime.getRuntime().halt(0);
        }
    }
}
