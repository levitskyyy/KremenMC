package kremenmc.bridge;

import java.util.ArrayList;
import java.util.Collection;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.function.Consumer;
import org.bukkit.Chunk;
import org.bukkit.Location;
import org.bukkit.Material;
import org.bukkit.Particle;
import org.bukkit.World;
import org.bukkit.block.Block;
import org.bukkit.block.data.BlockData;
import org.bukkit.entity.Entity;
import org.bukkit.entity.EntityType;
import org.bukkit.entity.Item;
import org.bukkit.entity.LivingEntity;
import org.bukkit.event.entity.CreatureSpawnEvent;
import org.bukkit.event.entity.EntitySpawnEvent;
import org.bukkit.inventory.ItemStack;

/** World methods for heightmaps, chunks, entities, particles and block data. */
abstract class WorldExtras implements Particles.Spawner {
    final Bridge wb;
    final String wname;

    WorldExtras(Bridge b, String name) {
        this.wb = b;
        this.wname = name;
    }

    private World world() { return wb.worlds.get(wname); }

    public int getHighestBlockYAt(int x, int z) { return wb.query("highest", "world", wname, "x", x, "z", z) instanceof Number n ? n.intValue() : 0; }
    public int getHighestBlockYAt(Location l) { return getHighestBlockYAt(l.getBlockX(), l.getBlockZ()); }
    public int getHighestBlockYAt(int x, int z, org.bukkit.HeightMap h) { return getHighestBlockYAt(x, z); }
    public Block getHighestBlockAt(int x, int z) { return Worlds.block(wb, world(), x, getHighestBlockYAt(x, z), z, null); }
    public Block getHighestBlockAt(Location l) { return getHighestBlockAt(l.getBlockX(), l.getBlockZ()); }
    public Block getHighestBlockAt(int x, int z, org.bukkit.HeightMap h) { return getHighestBlockAt(x, z); }

    public BlockData getBlockData(int x, int y, int z) { return world().getBlockAt(x, y, z).getBlockData(); }
    public BlockData getBlockData(Location l) { return getBlockData(l.getBlockX(), l.getBlockY(), l.getBlockZ()); }
    public Material getType(int x, int y, int z) { return world().getBlockAt(x, y, z).getType(); }
    public Material getType(Location l) { return getType(l.getBlockX(), l.getBlockY(), l.getBlockZ()); }
    public void setBlockData(int x, int y, int z, BlockData d) { world().getBlockAt(x, y, z).setBlockData(d); }
    public void setBlockData(Location l, BlockData d) { setBlockData(l.getBlockX(), l.getBlockY(), l.getBlockZ(), d); }
    public void setType(int x, int y, int z, Material m) { world().getBlockAt(x, y, z).setType(m); }
    public void setType(Location l, Material m) { setType(l.getBlockX(), l.getBlockY(), l.getBlockZ(), m); }

    public boolean isChunkLoaded(int x, int z) { return Boolean.TRUE.equals(wb.query("chunk_loaded", "world", wname, "x", x, "z", z)); }
    public boolean isChunkLoaded(Chunk c) { return isChunkLoaded(c.getX(), c.getZ()); }
    public boolean isChunkGenerated(int x, int z) { return isChunkLoaded(x, z); }
    public Chunk getChunkAt(int x, int z) { return chunk(x, z); }
    public Chunk getChunkAt(int x, int z, boolean generate) { return chunk(x, z); }
    public Chunk getChunkAt(Location l) { return chunk(l.getBlockX() >> 4, l.getBlockZ() >> 4); }
    public Chunk getChunkAt(Block b) { return chunk(b.getX() >> 4, b.getZ() >> 4); }
    public Chunk getChunkAt(long key) { return chunk((int) key, (int) (key >> 32)); }

    public Chunk[] getLoadedChunks() {
        List<Chunk> out = new ArrayList<>();
        for (Object o : Wire.list(wb.query("loaded_chunks", "world", wname))) {
            Map<String, Object> m = Wire.map(o);
            out.add(chunk((int) Wire.lng(m, "x"), (int) Wire.lng(m, "z")));
        }
        return out.toArray(new Chunk[0]);
    }

    Chunk chunk(int x, int z) { return Stubs.proxy(Chunk.class, new ChunkImpl(this, x, z)); }

    public static final class ChunkImpl {
        final WorldExtras w;
        final int x, z;

        ChunkImpl(WorldExtras w, int x, int z) {
            this.w = w;
            this.x = x;
            this.z = z;
        }

        public int getX() { return x; }
        public int getZ() { return z; }
        public World getWorld() { return w.world(); }
        public boolean isLoaded() { return w.isChunkLoaded(x, z); }
        public boolean isGenerated() { return isLoaded(); }
        public boolean load() { return isLoaded(); }
        public boolean load(boolean generate) { return isLoaded(); }
        public long getChunkKey() { return ((long) z << 32) | (x & 0xFFFFFFFFL); }
        public Block getBlock(int bx, int by, int bz) { return Worlds.block(w.wb, w.world(), (x << 4) + (bx & 15), by, (z << 4) + (bz & 15), null); }

        public Entity[] getEntities() {
            List<Entity> out = new ArrayList<>();
            for (Entity e : w.getEntities()) {
                Location l = e.getLocation();
                if (l.getBlockX() >> 4 == x && l.getBlockZ() >> 4 == z) out.add(e);
            }
            return out.toArray(new Entity[0]);
        }

        @Override public boolean equals(Object o) { return o instanceof ChunkImpl c && c.x == x && c.z == z && c.w.wname.equals(w.wname); }
        @Override public int hashCode() { return (x * 31 + z) * 31 + w.wname.hashCode(); }
        @Override public String toString() { return "CraftChunk{x=" + x + ", z=" + z + "}"; }
    }

    public List<Entity> getEntities() { return Entities.list(wb, wb.query("entities", "world", wname)); }

    public List<LivingEntity> getLivingEntities() {
        List<LivingEntity> out = new ArrayList<>();
        for (Entity e : getEntities()) if (e instanceof LivingEntity l) out.add(l);
        return out;
    }

    @SuppressWarnings("unchecked")
    public <T extends Entity> Collection<T> getEntitiesByClass(Class<T> c) {
        List<T> out = new ArrayList<>();
        for (Entity e : getEntities()) if (c.isInstance(e)) out.add((T) e);
        return out;
    }

    public Collection<Entity> getEntitiesByClasses(Class<?>... cs) {
        List<Entity> out = new ArrayList<>();
        for (Entity e : getEntities()) {
            for (Class<?> c : cs) {
                if (c.isInstance(e)) {
                    out.add(e);
                    break;
                }
            }
        }
        return out;
    }

    public Collection<Entity> getNearbyEntities(Location l, double x, double y, double z) {
        List<Entity> out = new ArrayList<>();
        for (Entity e : Entities.list(wb, wb.query("entities", "world", wname, "x", l.getX(), "y", l.getY(), "z", l.getZ(), "radius", Math.sqrt(x * x + y * y + z * z)))) {
            Location o = e.getLocation();
            if (Math.abs(o.getX() - l.getX()) <= x && Math.abs(o.getY() - l.getY()) <= y && Math.abs(o.getZ() - l.getZ()) <= z) out.add(e);
        }
        return out;
    }

    public Entity spawnEntity(Location l, EntityType t) { return spawn(l, t, null); }
    public Entity spawnEntity(Location l, EntityType t, boolean randomize) { return spawn(l, t, null); }
    public Entity spawnEntity(Location l, EntityType t, CreatureSpawnEvent.SpawnReason r) { return spawn(l, t, null); }

    @SuppressWarnings("unchecked")
    public <T extends Entity> T spawn(Location l, Class<T> c) { return (T) spawn(l, typeOf(c), null); }

    @SuppressWarnings("unchecked")
    public <T extends Entity> T spawn(Location l, Class<T> c, Consumer<? super T> fn) { return (T) spawn(l, typeOf(c), (Consumer<Entity>) fn); }

    @SuppressWarnings("unchecked")
    public <T extends Entity> T spawn(Location l, Class<T> c, CreatureSpawnEvent.SpawnReason r, Consumer<? super T> fn) { return (T) spawn(l, typeOf(c), (Consumer<Entity>) fn); }

    @SuppressWarnings("unchecked")
    public <T extends Entity> T spawn(Location l, Class<T> c, boolean randomize, Consumer<? super T> fn) { return (T) spawn(l, typeOf(c), (Consumer<Entity>) fn); }

    static EntityType typeOf(Class<?> c) {
        for (EntityType t : EntityType.values()) if (t.getEntityClass() == c) return t;
        for (EntityType t : EntityType.values()) if (t.getEntityClass() != null && c.isAssignableFrom(t.getEntityClass())) return t;
        throw new IllegalArgumentException("cannot spawn entity of class " + c.getName());
    }

    private Entity spawn(Location l, EntityType t, Consumer<Entity> fn) {
        if (t == null || t == EntityType.UNKNOWN || t == EntityType.PLAYER) throw new IllegalArgumentException("cannot spawn " + t);
        Map<String, Object> m = Wire.map(wb.query("spawn_entity", "world", wname, "type", t.getKey().toString(),
                "x", l.getX(), "y", l.getY(), "z", l.getZ(), "yaw", (double) l.getYaw(), "pitch", (double) l.getPitch()));
        Entity e = Entities.of(wb, m);
        if (e == null) return null;
        if (fn != null) fn.accept(e);
        EntitySpawnEvent ev = e instanceof LivingEntity le ? new CreatureSpawnEvent(le, CreatureSpawnEvent.SpawnReason.CUSTOM) : new EntitySpawnEvent(e);
        wb.pluginManager.callEvent(ev);
        if (ev.isCancelled()) e.remove();
        return e;
    }

    public Item dropItem(Location l, ItemStack st) { return drop(l, st, false); }
    public Item dropItem(Location l, ItemStack st, Consumer<? super Item> fn) { Item i = drop(l, st, false); if (fn != null && i != null) fn.accept(i); return i; }
    public Item dropItemNaturally(Location l, ItemStack st) { return drop(l, st, true); }
    public Item dropItemNaturally(Location l, ItemStack st, Consumer<? super Item> fn) { Item i = drop(l, st, true); if (fn != null && i != null) fn.accept(i); return i; }

    private Item drop(Location l, ItemStack st, boolean naturally) {
        if (st == null || Worlds.Materials.isAir(st.getType())) throw new IllegalArgumentException("cannot drop air");
        double x = l.getX(), y = l.getY(), z = l.getZ();
        if (naturally) {
            x += Math.random() * 0.7 - 0.35;
            y += Math.random() * 0.7 - 0.35;
            z += Math.random() * 0.7 - 0.35;
        }
        Map<String, Object> m = Wire.map(wb.query("drop_item", "world", wname, "x", x, "y", y, "z", z,
                "item", st.getType().getKey().toString(), "count", st.getAmount(), "naturally", naturally));
        return Entities.of(wb, m) instanceof Item i ? i : null;
    }

    @Override
    public void particle(Particle p, double x, double y, double z, int count, double ox, double oy, double oz, double extra, Object data, boolean force) {
        Particles.spawn(wb, null, wname, p, x, y, z, count, ox, oy, oz, extra, data, force);
    }

    static BlockData blockData(String state) { return Stubs.proxy(BlockData.class, new BlockDataImpl(state)); }

    static BlockData blockData(Material m, String props) {
        String key = m.getKey().toString();
        if (props == null || props.isEmpty()) return blockData(key);
        return blockData(props.startsWith("[") ? key + props : key + "[" + props + "]");
    }

    /** Block data as a vanilla block state string such as minecraft:oak_log[axis=y]. */
    public static final class BlockDataImpl {
        final String state;

        BlockDataImpl(String state) {
            String s = state == null || state.isEmpty() ? "minecraft:air" : state.toLowerCase(Locale.ROOT);
            this.state = s.contains(":") && s.indexOf(':') < (s.contains("[") ? s.indexOf('[') : s.length()) ? s : "minecraft:" + s;
        }

        public Material getMaterial() { return Worlds.Materials.fromState(state); }
        public String getAsString() { return state; }
        public String getAsString(boolean hideUnspecified) { return state; }
        public BlockData clone() { return blockData(state); }
        public boolean matches(BlockData o) { return o != null && o.getMaterial() == getMaterial(); }
        public BlockData merge(BlockData o) { return o == null ? blockData(state) : blockData(o.getAsString()); }
        public boolean isSupported(Block b) { return true; }
        public boolean isSupported(Location l) { return true; }
        public boolean isRandomlyTicked() { return false; }
        public boolean requiresCorrectToolForDrops() { return false; }
        public Material getPlacementMaterial() { return getMaterial(); }

        @Override public boolean equals(Object o) { return o instanceof BlockDataImpl d && d.state.equals(state); }
        @Override public int hashCode() { return state.hashCode(); }
        @Override public String toString() { return "CraftBlockData{" + state + "}"; }
    }
}
