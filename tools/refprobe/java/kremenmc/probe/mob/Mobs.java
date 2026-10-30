package kremenmc.probe.mob;

import java.lang.reflect.Field;
import java.lang.reflect.Modifier;
import java.util.HashSet;
import net.minecraft.core.BlockPos;
import net.minecraft.core.Holder;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.network.syncher.EntityDataAccessor;
import net.minecraft.network.syncher.EntityDataSerializers;
import net.minecraft.network.syncher.SynchedEntityData;
import net.minecraft.world.Difficulty;
import net.minecraft.world.DifficultyInstance;
import net.minecraft.world.entity.Entity;
import net.minecraft.world.entity.EntitySpawnReason;
import net.minecraft.world.entity.EntityType;
import net.minecraft.world.entity.EquipmentSlot;
import net.minecraft.world.entity.LivingEntity;
import net.minecraft.world.entity.Mob;
import net.minecraft.world.item.ItemStack;

// Mobs prints, black-box: the synched data accessor ids of every entity
// class ("meta"), the class chain of every type ("class"), the non-default
// synched values of a fresh instance ("def"), and for the listed types N
// finalizeSpawn outcomes (baby, equipment with drop chances, non-default
// synched values, experience reward) under a given difficulty.
//
//	Mobs <n> <difficulty> <dayTime> <inhabited> <moon> <type>...
public final class Mobs {
    public static void main(String[] args) throws Exception {
        Env.boot();
        var o = Env.out;
        int n = Integer.parseInt(args[0]);
        Difficulty diff = Difficulty.byName(args[1]);
        long day = Long.parseLong(args[2]), inh = Long.parseLong(args[3]);
        float moon = Float.parseFloat(args[4]);
        Env.run(() -> {
            var seen = new HashSet<Class<?>>();
            for (EntityType<?> t : BuiltInRegistries.ENTITY_TYPE) {
                String key = EntityType.getKey(t).getPath();
                Entity e = null;
                try { e = t.create(Env.level, EntitySpawnReason.COMMAND); } catch (Throwable ex) {}
                if (e == null) continue;
                StringBuilder chain = new StringBuilder("class " + key);
                for (Class<?> c = e.getClass(); c != null && c != Object.class; c = c.getSuperclass()) {
                    chain.append(' ').append(c.getSimpleName());
                    if (seen.add(c)) {
                        for (Field f : c.getDeclaredFields()) {
                            if (!Modifier.isStatic(f.getModifiers()) || f.getType() != EntityDataAccessor.class) continue;
                            f.setAccessible(true);
                            EntityDataAccessor<?> a = (EntityDataAccessor<?>) f.get(null);
                            o.println("meta " + c.getSimpleName() + " " + f.getName() + " " + a.id() + " " + EntityDataSerializers.getSerializedId(a.serializer()));
                        }
                    }
                }
                o.println(chain);
                o.println("def " + key + values(e));
            }
            var di = new DifficultyInstance(diff, day, inh, moon);
            o.println("difficulty " + diff + " eff=" + di.getEffectiveDifficulty() + " special=" + di.getSpecialMultiplier());
            int first = 5;
            if (args.length > 5 && args[5].startsWith("biome=")) {
                first = 6;
                Env.command("fillbiome -8 -64 -8 56 0 56 " + args[5].substring(6));
                o.println("biome " + Env.level.getBiome(new BlockPos(0, -60, 0)).unwrapKey().get().identifier() + " " + Env.failures);
            }
            for (int i = first; i < args.length; i++) {
                var t = BuiltInRegistries.ENTITY_TYPE.getValue(net.minecraft.resources.Identifier.withDefaultNamespace(args[i]));
                for (int k = 0; k < n; k++) {
                    Entity e = t.create(Env.level, EntitySpawnReason.NATURAL);
                    e.setPos(0.5 + (k % 16) * 3, -60, 0.5 + (k / 16) * 3);
                    StringBuilder sb = new StringBuilder("fin " + args[i]);
                    if (e instanceof Mob m) {
                        m.finalizeSpawn(Env.level, di, EntitySpawnReason.NATURAL, null);
                        sb.append(" baby=").append(m.isBaby());
                        for (EquipmentSlot s : EquipmentSlot.values()) {
                            ItemStack st = m.getItemBySlot(s);
                            if (st.isEmpty()) continue;
                            sb.append(' ').append(s.getName()).append('=').append(BuiltInRegistries.ITEM.getKey(st.getItem()).getPath());
                            if (st.isEnchanted()) sb.append('*');
                            sb.append('@').append(m.getDropChances().byEquipment(s));
                        }
                        sb.append(" xp=").append(m.getExperienceReward(Env.level, null));
                        if (m.getVehicle() != null) sb.append(" vehicle=").append(EntityType.getKey(m.getVehicle().getType()).getPath());
                        if (!m.getPassengers().isEmpty()) sb.append(" passenger=").append(EntityType.getKey(m.getPassengers().get(0).getType()).getPath());
                    }
                    sb.append(" |").append(values(e));
                    o.println(sb);
                }
            }
        });
        o.flush();
        Runtime.getRuntime().halt(0);
    }

    static String values(Entity e) {
        StringBuilder sb = new StringBuilder();
        var vs = e.getEntityData().getNonDefaultValues();
        if (vs == null) return "";
        for (SynchedEntityData.DataValue<?> v : vs) {
            Object x = v.value();
            String s;
            if (x instanceof Holder<?> h) s = h.unwrapKey().map(k -> k.identifier().toString()).orElse("?");
            else s = String.valueOf(x).replace(' ', '_');
            sb.append(' ').append(v.id()).append(':').append(EntityDataSerializers.getSerializedId(v.serializer())).append('=').append(s);
        }
        return sb.toString();
    }
}
