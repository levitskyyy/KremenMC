package kremenmc.probe.oruin;

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
import net.minecraft.world.level.levelgen.Heightmap;
import net.minecraft.world.level.levelgen.structure.BoundingBox;
import net.minecraft.world.level.levelgen.structure.StructureStart;
import net.minecraft.world.level.levelgen.structure.pieces.StructurePieceSerializationContext;
import net.minecraft.world.level.storage.LevelStorageSource;
import net.minecraft.world.phys.AABB;

// ORScene is kremenmc.probe.temple.TScene for several starts in one scene:
// the scene is loaded once, then every START/CHUNKS pair runs placeInChunk
// for its chunks in order (WorldgenRandom(xoroshiro) with the decoration
// seed + setFeatureSeed(index, step); level.getRandom() is the real
// worldgen_region_random at the chunk min corner). Writes are raw section
// writes (setBlock returns true, as a real WorldGenRegion does, see
// RegionSet); neighbour updates and scheduled ticks are dropped, but every
// scheduleTick is printed ("T x y z what delay"). Heightmap reads are
// answered from the scene as loaded (frozen; LIVEH=1: live blocks).
// SEALEVEL=<y> answers getSeaLevel. LOG=1 logs draws (R placement random,
// Q region random), LOGW=1 writes, READPATH=<s> reads whose call path
// contains s. Output per start: "RUN i", per chunk "CHUNK"/"NEXT cx cz
// <nextLong>", "AFTER i <start tag>"; at the end the changed blocks (B),
// block entities (BE) and entities (E, full NBT) of the region.
public final class ORScene {
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

    static int[] RG;
    static BlockState[] SCENE;
    static int TOP;

    static BlockState sceneAt(ServerLevel level, int x, int y, int z, boolean live) {
        if (!live && x >= RG[0] && x <= RG[3] && y >= RG[1] && y <= RG[4] && z >= RG[2] && z <= RG[5]) {
            int sx = RG[3] - RG[0] + 1, sz = RG[5] - RG[2] + 1;
            return SCENE[((y - RG[1]) * sz + (z - RG[2])) * sx + (x - RG[0])];
        }
        LevelChunk ch = level.getChunk(x >> 4, z >> 4);
        return ch.getSection(ch.getSectionIndex(y)).getBlockState(x & 15, y & 15, z & 15);
    }

    static int colHeight(ServerLevel level, Heightmap.Types t, int x, int z, boolean live) {
        for (int y = TOP; y >= level.getMinY(); y--) {
            if (t.isOpaque().test(sceneAt(level, x, y, z, live))) return y + 1;
        }
        return level.getMinY();
    }

    record Run(CompoundTag start, List<int[]> chunks) {}

    public static void main(String[] args) throws Exception {
        PrintStream out = new PrintStream(new java.io.FileOutputStream(java.io.FileDescriptor.out), true);
        System.setOut(System.err);
        LogWR.OUT = out;
        LogWR.LIMIT = System.getenv("PATHLIM") != null ? Integer.parseInt(System.getenv("PATHLIM")) : 4;
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        Path dir = Files.createTempDirectory(Path.of("").toAbsolutePath(), "orsceneworld-");
        var access = LevelStorageSource.createDefault(dir).createAccess("world");
        var packs = ServerPacksSource.createPackRepository(access);
        MinecraftServer server = MinecraftServer.spin(t -> GameTestServer.create(t, access, packs, Optional.of("minecraft:always_pass"), false, 1000000));
        for (int i = 0; i < 1200 && !server.isReady(); i++) Thread.sleep(50);
        var lines = Files.readAllLines(Path.of(args[0]));
        CompletableFuture<Void> f = new CompletableFuture<>();
        server.execute(() -> {
            try {
                ServerLevel level = server.overworld();
                long seed = 0;
                int index = 0, step = 0;
                List<BlockState> pal = new ArrayList<>();
                List<Run> runs = new ArrayList<>();
                CompoundTag start = null;
                for (int li = 0; li < lines.size(); li++) {
                    String l = lines.get(li);
                    String[] a = l.split(" ");
                    switch (a[0]) {
                        case "SEED" -> { seed = Long.parseLong(a[1]); index = Integer.parseInt(a[2]); step = Integer.parseInt(a[3]); }
                        case "REGION" -> { RG = new int[6]; for (int i = 0; i < 6; i++) RG[i] = Integer.parseInt(a[i + 1]); }
                        case "PAL" -> {
                            int n = Integer.parseInt(a[1]);
                            for (int i = 0; i < n; i++) pal.add(BlockStateParser.parseForBlock(level.holderLookup(Registries.BLOCK), lines.get(++li), false).blockState());
                        }
                        case "DATA" -> {
                            int sx = RG[3] - RG[0] + 1, sy = RG[4] - RG[1] + 1, sz = RG[5] - RG[2] + 1;
                            SCENE = new BlockState[sx * sy * sz];
                            int k = 0;
                            for (int i = 1; i < a.length; i++) {
                                String[] p = a[i].split("\\*");
                                BlockState s = pal.get(Integer.parseInt(p[0]));
                                int c = p.length > 1 ? Integer.parseInt(p[1]) : 1;
                                for (int j = 0; j < c; j++) SCENE[k++] = s;
                            }
                        }
                        case "START" -> start = TagParser.parseCompoundFully(l.substring(6));
                        case "CHUNKS" -> {
                            List<int[]> cs = new ArrayList<>();
                            for (int i = 1; i < a.length; i++) { String[] p = a[i].split(","); cs.add(new int[]{Integer.parseInt(p[0]), Integer.parseInt(p[1])}); }
                            runs.add(new Run(start, cs));
                        }
                        default -> {}
                    }
                }
                final int[] rg = RG;
                TOP = rg[4] + 40;
                for (int cx = rg[0] >> 4; cx <= rg[3] >> 4; cx++) for (int cz = rg[2] >> 4; cz <= rg[5] >> 4; cz++) level.getChunk(cx, cz);
                AABB ebox = new AABB(rg[0] - 2, rg[1] - 2, rg[2] - 2, rg[3] + 3, rg[4] + 3, rg[5] + 3);
                for (var e : level.getEntities((net.minecraft.world.entity.Entity) null, ebox, e -> true)) e.discard();
                int k = 0;
                for (int y = rg[1]; y <= rg[4]; y++) for (int z = rg[2]; z <= rg[5]; z++) for (int x = rg[0]; x <= rg[3]; x++) {
                    BlockPos p = new BlockPos(x, y, z);
                    level.removeBlockEntity(p);
                    LevelChunk ch = level.getChunk(x >> 4, z >> 4);
                    ch.getSection(ch.getSectionIndex(y)).setBlockState(x & 15, y & 15, z & 15, SCENE[k++], false);
                }
                out.println("LEVELSEED " + level.getSeed() + " BIOME " + level.getBiome(new BlockPos(rg[0], rg[1], rg[2])).unwrapKey().get().identifier());
                final boolean live = System.getenv("LIVEH") != null;
                final boolean rawChg = System.getenv("RAWCHG") != null;
                final boolean logw = System.getenv("LOGW") != null;
                final String readPath = System.getenv("READPATH");
                final Integer seaLevel = System.getenv("SEALEVEL") == null ? null : Integer.parseInt(System.getenv("SEALEVEL"));
                final long wseed = seed;
                var nset = level.registryAccess().lookupOrThrow(Registries.NOISE_SETTINGS).getOrThrow(net.minecraft.world.level.levelgen.NoiseGeneratorSettings.OVERWORLD).value();
                var wrs = net.minecraft.world.level.levelgen.RandomState.create(level.registryAccess().lookupOrThrow(Registries.NOISE), seed, nset);
                List<StructureStart> starts = new ArrayList<>();
                for (int ri = 0; ri < runs.size(); ri++) {
                    Run run = runs.get(ri);
                    StructureStart st = StructureStart.loadStaticStart(StructurePieceSerializationContext.fromLevel(level), run.start(), seed);
                    starts.add(st);
                    out.println("RUN " + ri + " PIECES " + st.getPieces().size());
                    for (int[] c : run.chunks()) {
                        LogWR r = new LogWR(new net.minecraft.world.level.levelgen.XoroshiroRandomSource(0));
                        long deco = r.setDecorationSeed(seed, c[0] << 4, c[1] << 4);
                        r.setFeatureSeed(deco, index, step);
                        r.on = System.getenv("LOG") != null;
                        out.println("CHUNK " + c[0] + " " + c[1]);
                        BoundingBox cb = new BoundingBox(c[0] << 4, level.getMinY(), c[1] << 4, (c[0] << 4) + 15, level.getMaxY(), (c[1] << 4) + 15);
                        final ServerLevel lvl = level;
                        final LogRS regionRand = new LogRS(wrs.getOrCreateRandomFactory(net.minecraft.resources.Identifier.parse("minecraft:worldgen_region_random")).at(c[0] << 4, 0, c[1] << 4));
                        regionRand.on = System.getenv("LOG") != null;
                        net.minecraft.world.level.WorldGenLevel wl = (net.minecraft.world.level.WorldGenLevel) java.lang.reflect.Proxy.newProxyInstance(ORScene.class.getClassLoader(),
                            new Class<?>[]{net.minecraft.world.level.WorldGenLevel.class}, (proxy, m, margs) -> {
                                String n = m.getName();
                                Object res;
                                // A real WorldGenRegion.setBlock returns true even when the state
                                // is unchanged (RegionSet); RAWCHG=1: false then (TScene).
                                if (n.equals("setBlock") || n.equals("setBlockAndUpdate")) res = rawChg ? rawSetChanged(lvl, (BlockPos) margs[0], (BlockState) margs[1]) : rawSet(lvl, (BlockPos) margs[0], (BlockState) margs[1]);
                                else if (n.equals("scheduleTick")) {
                                    BlockPos bp = (BlockPos) margs[0];
                                    Object what = margs[1];
                                    String w = what instanceof net.minecraft.world.level.block.Block b ? "block:" + BuiltInRegistries.BLOCK.getKey(b).getPath()
                                        : what instanceof net.minecraft.world.level.material.Fluid fl ? "fluid:" + BuiltInRegistries.FLUID.getKey(fl).getPath() : String.valueOf(what);
                                    out.println("T " + bp.getX() + " " + bp.getY() + " " + bp.getZ() + " " + w + " " + margs[2] + " @" + LogWR.path());
                                    res = null;
                                } else if (n.equals("updateNeighborsAt") || n.equals("blockUpdated") || n.equals("updateNeighbourForOutputSignal")) res = null;
                                else if (n.equals("getHeight") && margs != null && margs.length == 3) res = colHeight(lvl, (Heightmap.Types) margs[0], (Integer) margs[1], (Integer) margs[2], live);
                                else if (n.equals("getHeightmapPos")) { BlockPos bp = (BlockPos) margs[1]; res = new BlockPos(bp.getX(), colHeight(lvl, (Heightmap.Types) margs[0], bp.getX(), bp.getZ(), live), bp.getZ()); }
                                else if (n.equals("getRandom")) res = regionRand;
                                else if (n.equals("getSeed")) res = wseed;
                                else if (n.equals("getSeaLevel") && seaLevel != null) res = seaLevel;
                                else res = m.invoke(lvl, margs);
                                boolean write = n.equals("setBlock") || n.equals("setBlockAndUpdate") || n.equals("addFreshEntityWithPassengers") || n.equals("addFreshEntity")
                                    || (n.equals("getHeight") && margs != null && margs.length > 0) || n.equals("getSeaLevel");
                                boolean read = readPath != null && !write && !n.equals("scheduleTick") && LogWR.path().contains(readPath);
                                if ((logw && write) || read) {
                                    StringBuilder b = new StringBuilder("W ").append(n);
                                    if (margs != null) for (Object o : margs) {
                                        if (o instanceof BlockPos bp) b.append(' ').append(bp.getX()).append(',').append(bp.getY()).append(',').append(bp.getZ());
                                        else if (o instanceof BlockState bs) b.append(' ').append(state(bs));
                                        else b.append(' ').append(o);
                                    }
                                    if (res instanceof BlockState bs) b.append(" -> ").append(state(bs));
                                    else if (res instanceof net.minecraft.world.level.material.FluidState fs) b.append(" -> ").append(fs.isEmpty() ? "empty" : BuiltInRegistries.FLUID.getKey(fs.getType()).getPath() + fs.getAmount());
                                    else if (res != null) b.append(" -> ").append(res);
                                    b.append(" @").append(LogWR.path());
                                    out.println(b);
                                }
                                return res;
                            });
                        if (System.getenv("PERPIECE") == null) {
                            st.placeInChunk(wl, level.structureManager(), level.getChunkSource().getGenerator(), r, cb, new ChunkPos(c[0], c[1]));
                        } else {
                            // placeInChunk by hand, printing each piece's box min y after its postProcess.
                            var ps = st.getPieces();
                            for (int i = 0; i < ps.size(); i++) {
                                if (!ps.get(i).getBoundingBox().intersects(cb)) continue;
                                ps.get(i).postProcess(wl, level.structureManager(), level.getChunkSource().getGenerator(), r, cb, new ChunkPos(c[0], c[1]), BlockPos.ZERO);
                                out.println("PY " + i + " " + ps.get(i).getBoundingBox().minY());
                            }
                        }
                        r.on = false;
                        out.println("NEXT " + c[0] + " " + c[1] + " " + r.nextLong() + " " + regionRand.d.nextLong());
                    }
                    CompoundTag after = st.createTag(StructurePieceSerializationContext.fromLevel(level), new ChunkPos(run.start().getIntOr("ChunkX", 0), run.start().getIntOr("ChunkZ", 0)));
                    out.println("AFTER " + ri + " " + after);
                }
                StringBuilder sb = new StringBuilder();
                k = 0;
                for (int y = rg[1]; y <= rg[4]; y++) for (int z = rg[2]; z <= rg[5]; z++) for (int x = rg[0]; x <= rg[3]; x++) {
                    BlockPos p = new BlockPos(x, y, z);
                    LevelChunk rc = level.getChunk(x >> 4, z >> 4);
                    BlockState s = rc.getSection(rc.getSectionIndex(y)).getBlockState(x & 15, y & 15, z & 15);
                    if (s != SCENE[k++]) sb.append("B ").append(x).append(' ').append(y).append(' ').append(z).append(' ').append(state(s)).append('\n');
                    var be = level.getBlockEntity(p);
                    if (be != null) sb.append("BE ").append(x).append(' ').append(y).append(' ').append(z).append(' ').append(be.saveWithoutMetadata(level.registryAccess())).append('\n');
                }
                for (var e : level.getEntities((net.minecraft.world.entity.Entity) null, ebox, e -> true)) {
                    var to = net.minecraft.world.level.storage.TagValueOutput.createWithContext(net.minecraft.util.ProblemReporter.DISCARDING, level.registryAccess());
                    e.saveWithoutId(to);
                    CompoundTag et = to.buildResult();
                    for (String drop : new String[]{"UUID", "Motion", "FallDistance", "Fire", "Air", "OnGround", "PortalCooldown", "HurtTime", "DeathTime", "HurtByTimestamp", "AbsorptionAmount", "FallFlying", "Brain", "fall_distance"}) et.remove(drop);
                    sb.append("E ").append(BuiltInRegistries.ENTITY_TYPE.getKey(e.getType())).append(' ').append(e.getX()).append(' ').append(e.getY()).append(' ').append(e.getZ())
                        .append(' ').append(et).append('\n');
                }
                out.print(sb);
                f.complete(null);
            } catch (Throwable t) {
                t.printStackTrace();
                f.completeExceptionally(t);
            }
        });
        try {
            f.get();
        } finally {
            out.flush();
            Runtime.getRuntime().halt(0);
        }
    }
}
