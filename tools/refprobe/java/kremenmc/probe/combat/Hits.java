package kremenmc.probe.combat;

import java.lang.reflect.Field;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.*;
import net.minecraft.core.Holder;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.core.registries.Registries;
import net.minecraft.resources.Identifier;
import net.minecraft.resources.ResourceKey;
import net.minecraft.server.level.TicketType;
import net.minecraft.world.Difficulty;
import net.minecraft.world.InteractionHand;
import net.minecraft.world.damagesource.DamageSource;
import net.minecraft.world.effect.MobEffectInstance;
import net.minecraft.world.entity.Entity;
import net.minecraft.world.entity.EntitySpawnReason;
import net.minecraft.world.entity.EquipmentSlot;
import net.minecraft.world.entity.LivingEntity;
import net.minecraft.world.entity.Mob;
import net.minecraft.world.item.ItemStack;
import net.minecraft.world.level.ChunkPos;
import net.minecraft.world.level.GameType;
import net.minecraft.world.phys.Vec3;

// Hits runs damage and melee scenarios against the reference. Each script
// line "run <name> key=value..." builds a fresh attacker (fake player at
// apos facing +z) and victim, then performs the actions in order:
//   hurt=<damage_type>:<amount>[:player|zombie]   victim.hurtServer
//   attack                                       attacker.attack(victim)
//   fall=<distance>                              victim.causeFallDamage
//   inv=<ticks>                                  set victim invulnerableTime
// and prints the victim state after each action.
public final class Hits {
    static Field inv, strength, lastHurt;
    static java.lang.reflect.Method equipUpd;

    public static void main(String[] args) throws Exception {
        List<String> lines = Files.readAllLines(Path.of(args[0]));
        Env.boot();
        var o = Env.out;
        inv = Entity.class.getDeclaredField("invulnerableTime");
        inv.setAccessible(true);
        strength = LivingEntity.class.getDeclaredField("attackStrengthTicker");
        strength.setAccessible(true);
        lastHurt = LivingEntity.class.getDeclaredField("lastHurt");
        lastHurt.setAccessible(true);
        equipUpd = LivingEntity.class.getDeclaredMethod("detectEquipmentUpdates");
        equipUpd.setAccessible(true);
        Env.run(() -> {
            Env.command("gamerule spawn_mobs false");
            Env.command("gamerule random_tick_speed 0");
            for (int cx = -2; cx <= 2; cx++)
                for (int cz = -2; cz <= 2; cz++) Env.level.getChunkSource().addTicketWithRadius(TicketType.DRAGON, new ChunkPos(cx, cz), 2);
        });
        Env.waitTicks(20);
        Env.run(() -> Env.command("fill -8 100 -8 8 100 8 minecraft:stone"));
        Env.waitTicks(2);
        Env.run(() -> Env.server.tickRateManager().setFrozen(true));
        Env.waitTicks(2);
        for (String raw : lines) {
            String line = raw.strip();
            if (!line.startsWith("run ")) continue;
            String res = Env.call(() -> {
                try {
                    return run(line.split("\\s+"));
                } catch (Throwable t) {
                    return "error " + line + " " + t + "\n";
                }
            });
            o.print(res);
        }
        Env.exit();
    }

    static Vec3 vec(String s) {
        String[] f = s.split(",");
        return new Vec3(Double.parseDouble(f[0]), Double.parseDouble(f[1]), Double.parseDouble(f[2]));
    }

    static ItemStack stack(String spec) {
        String[] parts = spec.split("\\+");
        var it = BuiltInRegistries.ITEM.getValue(Identifier.parse(parts[0]));
        ItemStack s = new ItemStack(it);
        var reg = Env.level.registryAccess().lookupOrThrow(Registries.ENCHANTMENT);
        for (int i = 1; i < parts.length; i++) {
            String[] e = parts[i].split("@");
            s.enchant(reg.getOrThrow(ResourceKey.create(Registries.ENCHANTMENT, Identifier.parse(e[0]))), Integer.parseInt(e[1]));
        }
        return s;
    }

    static void effects(LivingEntity e, String spec) {
        for (String x : spec.split(",")) {
            String[] f = x.split("@");
            var h = BuiltInRegistries.MOB_EFFECT.get(Identifier.parse(f[0])).get();
            e.addEffect(new MobEffectInstance(h, 1000, Integer.parseInt(f[1])));
        }
    }

    static LivingEntity spawn(String type, Vec3 p, float yaw) {
        LivingEntity e;
        if (type.equals("player")) {
            FakePlayer fp = new FakePlayer(Env.level, "victim" + p.hashCode());
            fp.setGameMode(GameType.SURVIVAL);
            e = fp;
        } else {
            var t = BuiltInRegistries.ENTITY_TYPE.getValue(Identifier.parse(type));
            e = (LivingEntity) t.create(Env.level, EntitySpawnReason.COMMAND);
            if (e instanceof Mob m) Traj.stripAi(m);
        }
        e.snapTo(p.x, p.y, p.z, yaw, 0);
        e.setYHeadRot(yaw);
        e.setOnGround(true);
        if (!(e instanceof FakePlayer)) Env.level.addFreshEntity(e);
        return e;
    }

    static String state(LivingEntity v) throws Exception {
        Vec3 d = v.getDeltaMovement();
        StringBuilder sb = new StringBuilder();
        sb.append("hp=").append(v.getHealth()).append(" abs=").append(v.getAbsorptionAmount())
            .append(" vel=").append(d.x).append(',').append(d.y).append(',').append(d.z)
            .append(" inv=").append(v.damageCooldownTime).append(" hurt=").append(v.hurtTime).append(" last=").append(lastHurt.getFloat(v)).append(" dead=").append(v.isDeadOrDying());
        StringBuilder dur = new StringBuilder();
        for (EquipmentSlot s : new EquipmentSlot[] {EquipmentSlot.HEAD, EquipmentSlot.CHEST, EquipmentSlot.LEGS, EquipmentSlot.FEET, EquipmentSlot.OFFHAND}) {
            ItemStack it = v.getItemBySlot(s);
            if (!it.isEmpty() && it.isDamageableItem()) dur.append(s.getName()).append(':').append(it.getDamageValue()).append(',');
        }
        if (dur.length() > 0) sb.append(" dur=").append(dur, 0, dur.length() - 1);
        return sb.toString();
    }

    static String run(String[] f) throws Exception {
        var level = Env.level;
        String name = f[1];
        Map<String, String> kv = new LinkedHashMap<>();
        List<String> actions = new ArrayList<>();
        for (int i = 2; i < f.length; i++) {
            String[] p = f[i].split("=", 2);
            if (p[0].equals("hurt") || p[0].equals("attack") || p[0].equals("fall") || p[0].equals("inv")) actions.add(f[i]);
            else kv.put(p[0], p.length > 1 ? p[1] : "");
        }
        Env.server.setDifficulty(Difficulty.valueOf(kv.getOrDefault("diff", "normal").toUpperCase()), true);
        FakePlayer a = new FakePlayer(level, "attacker");
        a.setGameMode(GameType.SURVIVAL);
        Vec3 ap = vec(kv.getOrDefault("apos", "0.5,101,0.5"));
        a.snapTo(ap.x, ap.y, ap.z, Float.parseFloat(kv.getOrDefault("ayaw", "0")), 0);
        a.setOnGround(true);
        if (kv.containsKey("weapon")) a.setItemInHand(InteractionHand.MAIN_HAND, stack(kv.get("weapon")));
        if (kv.containsKey("aeffect")) effects(a, kv.get("aeffect"));
        strength.setInt(a, Integer.parseInt(kv.getOrDefault("cooldown", "100")));
        if (kv.containsKey("crit")) {
            a.fallDistance = 1;
            a.setOnGround(false);
            a.setDeltaMovement(0, -0.1, 0);
        }
        if (kv.containsKey("sprint")) a.setSprinting(true);
        if (kv.containsKey("avel")) a.setDeltaMovement(vec(kv.get("avel")));
        Vec3 vp = vec(kv.getOrDefault("vpos", "0.5,101,2.5"));
        LivingEntity v = spawn(kv.getOrDefault("victim", "minecraft:zombie"), vp, Float.parseFloat(kv.getOrDefault("vyaw", "180")));
        List<LivingEntity> extra = new ArrayList<>();
        if (kv.containsKey("extra")) for (String p : kv.get("extra").split(";")) extra.add(spawn("minecraft:zombie", vec(p), 180));
        if (kv.containsKey("vhealth")) v.setHealth(Float.parseFloat(kv.get("vhealth")));
        if (kv.containsKey("vabs")) v.setAbsorptionAmount(Float.parseFloat(kv.get("vabs")));
        if (kv.containsKey("armor")) {
            for (String p : kv.get("armor").split(",")) {
                String[] q = p.split("/", 2);
                v.setItemSlot(EquipmentSlot.byName(q[0]), stack(q[1]));
            }
        }
        if (kv.containsKey("veffect")) effects(v, kv.get("veffect"));
        if (kv.containsKey("vfall")) v.fallDistance = Double.parseDouble(kv.get("vfall"));
        if (kv.containsKey("block") && v instanceof FakePlayer vpl) {
            vpl.setItemInHand(InteractionHand.OFF_HAND, stack("minecraft:shield"));
            vpl.startUsingItem(InteractionHand.OFF_HAND);
            vpl.tickUse(10);
        }
        equipUpd.invoke(a);
        equipUpd.invoke(v);
        StringBuilder sb = new StringBuilder();
        sb.append("res ").append(name).append(" start ").append(state(v)).append('\n');
        for (String act : actions) {
            String[] p = act.split("=", 2);
            String ret = "";
            switch (p[0]) {
                case "hurt" -> {
                    String[] q = p[1].split("/");
                    var dt = level.registryAccess().lookupOrThrow(Registries.DAMAGE_TYPE).getOrThrow(ResourceKey.create(Registries.DAMAGE_TYPE, Identifier.parse(q[0])));
                    DamageSource src;
                    if (q.length > 2 && q[2].equals("player")) src = new DamageSource(dt, a);
                    else if (q.length > 2 && q[2].equals("zombie")) {
                        var z = BuiltInRegistries.ENTITY_TYPE.getValue(Identifier.parse("minecraft:zombie")).create(level, EntitySpawnReason.COMMAND);
                        z.snapTo(ap.x, ap.y, ap.z, 0, 0);
                        src = new DamageSource(dt, z);
                    } else src = new DamageSource(dt);
                    ret = "ret=" + v.hurtServer(level, src, Float.parseFloat(q[1])) + " ";
                }
                case "attack" -> {
                    a.attack(v);
                    Vec3 av = a.getDeltaMovement();
                    ret = "avel=" + av.x + "," + av.y + "," + av.z + " asprint=" + a.isSprinting() + " ";
                }
                case "fall" -> ret = "ret=" + v.causeFallDamage(Double.parseDouble(p[1]), 1.0f, level.damageSources().fall()) + " ";
                case "inv" -> v.damageCooldownTime = Integer.parseInt(p[1]);
            }
            sb.append("res ").append(name).append(' ').append(act).append(' ').append(ret).append(state(v)).append('\n');
        }
        for (int i = 0; i < extra.size(); i++) sb.append("res ").append(name).append(" extra").append(i).append(' ').append(state(extra.get(i))).append('\n');
        v.discard();
        for (var e : extra) e.discard();
        a.discard();
        return sb.toString();
    }
}
