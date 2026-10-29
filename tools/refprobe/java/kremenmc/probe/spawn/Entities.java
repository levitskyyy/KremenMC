package kremenmc.probe.spawn;

import java.lang.reflect.Method;
import java.util.*;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.world.entity.EntitySpawnReason;
import net.minecraft.world.entity.EntityType;
import net.minecraft.world.entity.Mob;
import net.minecraft.world.level.block.Block;
import net.minecraft.world.level.block.state.BlockState;

// Entities prints per-mob instance properties relevant to spawning and
// despawning (cluster size, group limits, far-away removal, persistence),
// which classes declare the spawn-related overrides (signature level only),
// and the block states each type considers dangerous to spawn in.
public final class Entities {
    static final String[] METHODS = {"checkSpawnRules", "checkSpawnObstruction", "getWalkTargetValue", "removeWhenFarAway",
        "requiresCustomPersistence", "getMaxSpawnClusterSize", "isMaxGroupSizeReached", "finalizeSpawn", "checkDespawn"};

    public static void main(String[] args) throws Exception {
        Env.boot();
        var o = Env.out;
        Env.run(() -> {
            var level = Env.level;
            Map<String, List<String>> dangerGroups = new LinkedHashMap<>();
            for (EntityType<?> t : BuiltInRegistries.ENTITY_TYPE) {
                String key = EntityType.getKey(t).toString();
                StringBuilder ds = new StringBuilder();
                int run = -1, prev = -2;
                for (BlockState s : Block.BLOCK_STATE_REGISTRY) {
                    if (!t.isBlockDangerous(s)) continue;
                    int id = Block.getId(s);
                    if (id != prev + 1) {
                        if (run >= 0) ds.append(run).append('-').append(prev).append(' ');
                        run = id;
                    }
                    prev = id;
                }
                if (run >= 0) ds.append(run).append('-').append(prev);
                dangerGroups.computeIfAbsent(ds.toString(), k -> new ArrayList<>()).add(key);
                var e = t.create(level, EntitySpawnReason.NATURAL);
                if (!(e instanceof Mob m)) {
                    if (e != null) e.discard();
                    continue;
                }
                StringBuilder sb = new StringBuilder("mob " + key);
                sb.append(" cluster=").append(m.getMaxSpawnClusterSize());
                StringBuilder g = new StringBuilder();
                for (int k = 0; k <= 32; k++) if (m.isMaxGroupSizeReached(k)) g.append(k).append(',');
                sb.append(" groupReached=").append(g.length() == 0 ? "none" : g.substring(0, g.length() - 1));
                sb.append(" farAway0=").append(m.removeWhenFarAway(0)).append(" farAway20000=").append(m.removeWhenFarAway(20000));
                sb.append(" customPersist=").append(m.requiresCustomPersistence()).append(" persistRequired=").append(m.isPersistenceRequired());
                sb.append(" class=").append(m.getClass().getSimpleName());
                List<String> chain = new ArrayList<>();
                for (Class<?> c = m.getClass(); c != null && c != Object.class; c = c.getSuperclass()) chain.add(c.getSimpleName());
                sb.append(" chain=").append(String.join(">", chain));
                o.println(sb);
                StringBuilder ov = new StringBuilder("overrides " + key);
                for (String name : METHODS) {
                    for (Class<?> c = m.getClass(); c != null; c = c.getSuperclass()) {
                        boolean found = false;
                        for (Method mm : c.getDeclaredMethods()) {
                            if (mm.getName().equals(name) && !mm.isBridge() && !mm.isSynthetic()) found = true;
                        }
                        if (found) {
                            ov.append(' ').append(name).append('@').append(c.getSimpleName());
                            break;
                        }
                    }
                }
                o.println(ov);
                m.discard();
            }
            int i = 0;
            for (var en : dangerGroups.entrySet()) {
                o.println("danger " + (i++) + " types=" + String.join(",", en.getValue()));
                o.println("dangerstates " + en.getKey());
            }
        });
        Env.exit();
    }
}
