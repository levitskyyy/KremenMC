package kremenmc.bridge;

import java.util.ArrayList;
import java.util.HashMap;
import java.util.List;
import java.util.Locale;
import java.util.Map;
import java.util.UUID;
import java.util.concurrent.ConcurrentHashMap;
import org.bukkit.Location;
import org.bukkit.Material;
import org.bukkit.NamespacedKey;
import org.bukkit.World;
import org.bukkit.block.Block;
import org.bukkit.block.BlockFace;
import org.bukkit.block.BlockState;
import org.bukkit.entity.Player;

/** World and block proxies backed by Go. */
final class Worlds {
    private final Bridge b;
    private final Map<String, World> byName = new ConcurrentHashMap<>();

    Worlds(Bridge b) { this.b = b; }

    List<World> all() {
        List<World> out = new ArrayList<>();
        for (Object o : Wire.list(b.call(Wire.OP_WORLDS).get(0))) {
            Map<String, Object> m = Wire.map(o);
            String name = Wire.str(m, "name");
            World w = byName.computeIfAbsent(name, n -> Stubs.proxy(World.class, new WorldImpl(b, n)));
            ((WorldImpl) Stubs.implOf(w)).info = m;
            out.add(w);
        }
        return out;
    }

    World get(String name) {
        if (name == null || name.isEmpty()) return null;
        World w = byName.get(name);
        if (w != null) return w;
        for (World x : all()) if (x.getName().equals(name)) return x;
        return null;
    }

    World get(UUID id) {
        for (World x : all()) if (id.equals(x.getUID())) return x;
        return null;
    }

    static Block block(Bridge b, World w, int x, int y, int z, String knownState) {
        return Stubs.proxy(Block.class, new BlockImpl(b, w, x, y, z, knownState));
    }

    public static final class WorldImpl {
        final Bridge b;
        final String name;
        volatile Map<String, Object> info = Map.of();

        WorldImpl(Bridge b, String name) {
            this.b = b;
            this.name = name;
        }

        private World self() { return b.worlds.get(name); }

        public String getName() { return name; }
        public UUID getUID() { return info.get("uuid") instanceof UUID u ? u : UUID.nameUUIDFromBytes(name.getBytes()); }
        public NamespacedKey getKey() { return key(); }

        public NamespacedKey key() {
            String n = name.toLowerCase(Locale.ROOT);
            return n.contains(":") ? NamespacedKey.fromString(n) : NamespacedKey.minecraft(n);
        }

        public World.Environment getEnvironment() {
            try {
                return World.Environment.valueOf(Wire.str(info, "environment"));
            } catch (IllegalArgumentException e) {
                return World.Environment.NORMAL;
            }
        }

        public long getSeed() { return Wire.lng(info, "seed"); }
        public int getMinHeight() { return (int) Wire.lng(info, "minY"); }
        public int getMaxHeight() { return (int) Wire.lng(info, "maxY"); }
        public Location getSpawnLocation() { return Players.location(b, Wire.map(info.get("spawn"))); }

        public Block getBlockAt(int x, int y, int z) { return block(b, self(), x, y, z, null); }
        public Block getBlockAt(Location l) { return getBlockAt(l.getBlockX(), l.getBlockY(), l.getBlockZ()); }

        public List<Player> getPlayers() {
            List<Player> out = new ArrayList<>();
            for (Player p : b.players.online()) {
                Map<String, Object> s = ((Players.PlayerImpl) Stubs.implOf(p)).snapshot;
                if (name.equals(Wire.str(s, "world"))) out.add(p);
            }
            return out;
        }

        @Override public boolean equals(Object o) { return o instanceof WorldImpl w && w.name.equals(name); }
        @Override public int hashCode() { return name.hashCode(); }
        @Override public String toString() { return "CraftWorld{name=" + name + "}"; }
    }

    public static final class BlockImpl {
        final Bridge b;
        final World world;
        final int x, y, z;
        volatile String state;

        BlockImpl(Bridge b, World world, int x, int y, int z, String state) {
            this.b = b;
            this.world = world;
            this.x = x;
            this.y = y;
            this.z = z;
            this.state = state;
        }

        String state() {
            String s = state;
            if (s == null) {
                s = Wire.str(b.call(Wire.OP_GET_BLOCK, world.getName(), x, y, z), 0);
                state = s;
            }
            return s;
        }

        public Material getType() { return Materials.fromState(state()); }
        public int getX() { return x; }
        public int getY() { return y; }
        public int getZ() { return z; }
        public World getWorld() { return world; }
        public Location getLocation() { return new Location(world, x, y, z); }

        public Location getLocation(Location into) {
            if (into == null) return null;
            into.setWorld(world);
            into.set(x, y, z);
            into.setYaw(0);
            into.setPitch(0);
            return into;
        }

        public long getBlockKey() { return ((long) x & 0x7FFFFFF) | (((long) z & 0x7FFFFFF) << 27) | ((long) y << 54); }
        public Block getRelative(int dx, int dy, int dz) { return block(b, world, x + dx, y + dy, z + dz, null); }
        public Block getRelative(BlockFace f) { return getRelative(f, 1); }
        public Block getRelative(BlockFace f, int n) { return getRelative(f.getModX() * n, f.getModY() * n, f.getModZ() * n); }

        public BlockFace getFace(Block other) {
            for (BlockFace f : BlockFace.values()) {
                if (x + f.getModX() == other.getX() && y + f.getModY() == other.getY() && z + f.getModZ() == other.getZ()) return f;
            }
            return null;
        }

        public void setType(Material m) { setType(m, true); }

        public void setType(Material m, boolean physics) {
            String s = Materials.state(m);
            b.conn.notify(Wire.OP_SET_BLOCK, world.getName(), x, y, z, s, physics);
            state = s;
        }

        public boolean isEmpty() { return Materials.isAir(getType()); }
        public boolean isLiquid() { Material t = getType(); return t == Material.WATER || t == Material.LAVA; }
        public BlockState getState() { return Worlds.state(this, state()); }
        public BlockState getState(boolean snapshot) { return getState(); }

        @Override public boolean equals(Object o) { return o instanceof BlockImpl k && k.x == x && k.y == y && k.z == z && k.world.equals(world); }
        @Override public int hashCode() { return ((y * 31 + x) * 31 + z) * 31 + world.hashCode(); }
        @Override public String toString() { return "CraftBlock{pos=" + x + "," + y + "," + z + ",type=" + state + "}"; }
    }

    static BlockState state(BlockImpl block, String st) {
        return Stubs.proxy(BlockState.class, new StateImpl(block, st));
    }

    public static final class StateImpl {
        final BlockImpl block;
        volatile String state;

        StateImpl(BlockImpl block, String state) {
            this.block = block;
            this.state = state;
        }

        public Material getType() { return Materials.fromState(state); }
        public void setType(Material m) { state = Materials.state(m); }
        public Block getBlock() { return block(block.b, block.world, block.x, block.y, block.z, null); }
        public World getWorld() { return block.world; }
        public int getX() { return block.x; }
        public int getY() { return block.y; }
        public int getZ() { return block.z; }
        public Location getLocation() { return block.getLocation(); }
        public Location getLocation(Location l) { return block.getLocation(l); }
        public boolean isPlaced() { return true; }
        public boolean update() { return update(false, true); }
        public boolean update(boolean force) { return update(force, true); }

        public boolean update(boolean force, boolean physics) {
            block.setType(getType(), physics);
            return true;
        }
    }

    /** Material ↔ block state by namespaced key. Properties are dropped on read and defaulted on write. */
    static final class Materials {
        private static final Map<String, Material> BY_KEY = new HashMap<>();

        static {
            for (Material m : Material.values()) {
                if (!m.name().startsWith("LEGACY_")) BY_KEY.put("minecraft:" + m.name().toLowerCase(Locale.ROOT), m);
            }
        }

        static Material fromState(String state) {
            if (state == null || state.isEmpty()) return Material.AIR;
            int i = state.indexOf('[');
            String key = (i >= 0 ? state.substring(0, i) : state).toLowerCase(Locale.ROOT);
            if (!key.contains(":")) key = "minecraft:" + key;
            Material m = BY_KEY.get(key);
            return m == null ? Material.AIR : m;
        }

        static String state(Material m) {
            return "minecraft:" + m.name().toLowerCase(Locale.ROOT);
        }

        static boolean isAir(Material m) {
            return m == Material.AIR || m == Material.CAVE_AIR || m == Material.VOID_AIR;
        }
    }
}
