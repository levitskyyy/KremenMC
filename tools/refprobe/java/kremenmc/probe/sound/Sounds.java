package kremenmc.probe.sound;

import java.lang.reflect.Field;
import java.util.*;
import com.mojang.authlib.GameProfile;
import io.netty.channel.embedded.EmbeddedChannel;
import net.minecraft.core.BlockPos;
import net.minecraft.core.Direction;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.network.Connection;
import net.minecraft.network.protocol.Packet;
import net.minecraft.network.protocol.PacketFlow;
import net.minecraft.network.protocol.game.*;
import net.minecraft.resources.Identifier;
import net.minecraft.server.level.ClientInformation;
import net.minecraft.server.level.ServerPlayer;
import net.minecraft.server.level.TicketType;
import net.minecraft.server.network.CommonListenerCookie;
import net.minecraft.world.InteractionHand;
import net.minecraft.world.entity.*;
import net.minecraft.world.entity.item.ItemEntity;
import net.minecraft.world.item.ItemStack;
import net.minecraft.world.level.ChunkPos;
import net.minecraft.world.level.GameType;
import net.minecraft.world.phys.BlockHitResult;
import net.minecraft.world.phys.Vec3;

// Sounds records the sound packets (and block level events) an observer
// player receives in a set of scenarios: mob hurt/death/ambient/step, player
// hurt variants, falls, block place/break, item and xp pickup, level-up.
//
//	snd <scenario> <trial> <to> <kind> <name> <category> <volume> <pitch> [extra]
public final class Sounds {
    static final Map<ServerPlayer, EmbeddedChannel> chans = new HashMap<>();
    static ServerPlayer obs;
    static Field strength;

    public static void main(String[] args) throws Exception {
        Env.boot();
        strength = LivingEntity.class.getDeclaredField("attackStrengthTicker");
        strength.setAccessible(true);
        Env.run(() -> {
            Env.command("gamerule spawn_mobs false");
            Env.command("gamerule random_tick_speed 0");
            for (int cx = -2; cx <= 2; cx++)
                for (int cz = -2; cz <= 2; cz++) Env.level.getChunkSource().addTicketWithRadius(TicketType.DRAGON, new ChunkPos(cx, cz), 2);
        });
        Env.waitTicks(20);
        Env.run(() -> Env.command("fill -16 100 -16 16 100 16 minecraft:stone"));
        Env.waitTicks(2);
        Env.run(() -> Env.server.tickRateManager().setFrozen(true));
        Env.waitTicks(2);
        Set<String> only = new HashSet<>(Arrays.asList(args));
        Env.run(() -> {
            obs = join("watcher");
            obs.snapTo(0.5, 101, -6.5, 0, 0);
            obs.setGameMode(GameType.CREATIVE);
            for (String sc : new String[] {"zombiehit", "babyhit", "zombiekill", "golem", "playerhurt", "fall", "place", "break", "step", "pickup", "ambient", "variants", "babies"}) {
                if (!only.isEmpty() && !only.contains(sc)) continue;
                try {
                    scenario(sc);
                } catch (Throwable t) {
                    Env.out.println("error " + sc + " " + t);
                    t.printStackTrace(System.err);
                }
            }
        });
        Env.exit();
    }

    static ServerPlayer join(String name) {
        var prof = new GameProfile(UUID.nameUUIDFromBytes(name.getBytes()), name);
        Connection conn = new Connection(PacketFlow.SERVERBOUND);
        EmbeddedChannel ch = new EmbeddedChannel(conn);
        ServerPlayer p = new ServerPlayer(Env.server, Env.level, prof, ClientInformation.createDefault());
        Env.server.getPlayerList().placeNewPlayer(conn, p, CommonListenerCookie.createInitial(prof, false));
        chans.put(p, ch);
        p.connection.handleAcceptPlayerLoad(new ServerboundPlayerLoadedPacket());
        drain(p);
        return p;
    }

    static List<Packet<?>> drain(ServerPlayer p) {
        List<Packet<?>> out = new ArrayList<>();
        Object o;
        while ((o = chans.get(p).readOutbound()) != null) {
            if (o instanceof Packet<?> pk) {
                if (pk instanceof net.minecraft.network.protocol.BundlePacket<?> b) for (var x : b.subPackets()) out.add(x);
                else out.add(pk);
            }
        }
        return out;
    }

    static void report(String sc, int trial, String to, ServerPlayer p) {
        for (Packet<?> pk : drain(p)) {
            String s = null;
            if (pk instanceof ClientboundSoundPacket q)
                s = "pos " + q.getSound().value().location() + " " + q.getSource().getName() + " " + q.getVolume() + " " + q.getPitch() + " " + q.getX() + "," + q.getY() + "," + q.getZ();
            else if (pk instanceof ClientboundSoundEntityPacket q)
                s = "ent " + q.getSound().value().location() + " " + q.getSource().getName() + " " + q.getVolume() + " " + q.getPitch() + " id" + q.getId();
            else if (pk instanceof ClientboundLevelEventPacket q)
                s = "lev " + q.getType() + " " + q.getData() + " " + q.getPos().toShortString();
            else if (pk instanceof ClientboundTakeItemEntityPacket q)
                s = "take " + q.getItemId() + " " + q.getPlayerId() + " " + q.getAmount();
            else if (pk instanceof ClientboundEntityEventPacket q)
                s = "eev " + q.getEventId();
            if (s != null) Env.out.println("snd " + sc + " " + trial + " " + to + " " + s);
        }
    }

    static ServerPlayer actor(int i) {
        ServerPlayer a = join("actor" + i);
        a.setGameMode(GameType.SURVIVAL);
        a.snapTo(0.5, 101, 0.5, 0, 0);
        a.setOnGround(true);
        drain(a);
        drain(obs);
        return a;
    }

    static void leave(ServerPlayer a) {
        Env.server.getPlayerList().remove(a);
        chans.remove(a);
        drain(obs);
    }

    static Mob mob(String type, double x, double z) {
        var t = BuiltInRegistries.ENTITY_TYPE.getValue(Identifier.parse(type));
        Mob m = (Mob) t.create(Env.level, EntitySpawnReason.COMMAND);
        m.snapTo(x, 101, z, 180, 0);
        m.setOnGround(true);
        Env.level.addFreshEntity(m);
        return m;
    }

    static void stripAi(Mob m) {
        try {
            for (String fn : new String[] {"goalSelector", "targetSelector"}) {
                var f = Mob.class.getDeclaredField(fn);
                f.setAccessible(true);
                ((net.minecraft.world.entity.ai.goal.GoalSelector) f.get(m)).removeAllGoals(g -> true);
            }
            m.getBrain().removeAllBehaviors();
        } catch (ReflectiveOperationException ex) {
            throw new RuntimeException(ex);
        }
    }

    static void scenario(String sc) throws Exception {
        var level = Env.level;
        int n = 12;
        switch (sc) {
            case "zombiehit", "babyhit", "zombiekill" -> {
                for (int i = 0; i < n; i++) {
                    ServerPlayer a = actor(i);
                    Mob z = mob("minecraft:zombie", 0.5, 2.5);
                    if (sc.equals("babyhit")) z.setBaby(true);
                    if (sc.equals("zombiekill")) z.setHealth(1);
                    drain(a);
                    drain(obs);
                    strength.setInt(a, 100);
                    a.attack(z);
                    report(sc, i, "obs", obs);
                    report(sc, i, "self", a);
                    z.discard();
                    leave(a);
                }
            }
            case "golem" -> {
                for (int i = 0; i < 4; i++) {
                    Mob g = mob("minecraft:iron_golem", 0.5, 2.5);
                    Mob z = mob("minecraft:zombie", 0.5, 4);
                    drain(obs);
                    g.doHurtTarget(level, z);
                    report(sc, i, "obs", obs);
                    g.discard();
                    z.discard();
                }
            }
            case "playerhurt" -> {
                String[] types = {"generic", "drown", "on_fire", "freeze", "cactus", "starve", "sweet_berry_bush", "generic_kill"};
                int i = 0;
                for (String t : types) {
                    for (int k = 0; k < 3; k++) {
                        ServerPlayer a = actor(100 + i);
                        var dt = level.registryAccess().lookupOrThrow(net.minecraft.core.registries.Registries.DAMAGE_TYPE)
                            .getOrThrow(net.minecraft.resources.ResourceKey.create(net.minecraft.core.registries.Registries.DAMAGE_TYPE, Identifier.withDefaultNamespace(t)));
                        a.hurtServer(level, new net.minecraft.world.damagesource.DamageSource(dt), t.equals("generic_kill") ? 100 : 2);
                        Env.out.println("case playerhurt " + t + " " + k);
                        report(sc + "_" + t, k, "obs", obs);
                        report(sc + "_" + t, k, "self", a);
                        leave(a);
                        i++;
                    }
                }
            }
            case "fall" -> {
                double[] ds = {3.5, 4.5, 6, 8, 12};
                int i = 0;
                for (double d : ds) {
                    ServerPlayer a = actor(200 + i++);
                    a.causeFallDamage(d, 1.0f, level.damageSources().fall());
                    report(sc + "_" + d, 0, "obs", obs);
                    report(sc + "_" + d, 0, "self", a);
                    Mob z = mob("minecraft:zombie", 3.5, 3.5);
                    drain(obs);
                    z.causeFallDamage(d, 1.0f, level.damageSources().fall());
                    report(sc + "_zombie_" + d, 0, "obs", obs);
                    z.discard();
                    leave(a);
                }
            }
            case "place", "break" -> {
                String[] items = {"minecraft:stone", "minecraft:oak_planks", "minecraft:grass_block", "minecraft:glass", "minecraft:sand", "minecraft:white_wool", "minecraft:gravel", "minecraft:oak_log"};
                int i = 0;
                for (String it : items) {
                    for (int k = 0; k < (sc.equals("place") ? 3 : 1); k++) {
                        ServerPlayer a = actor(300 + i++);
                        BlockPos target = new BlockPos(2, 100, 2);
                        BlockPos above = target.above();
                        level.setBlock(above, net.minecraft.world.level.block.Blocks.AIR.defaultBlockState(), 3);
                        if (sc.equals("break")) {
                            level.setBlock(above, BuiltInRegistries.BLOCK.getValue(Identifier.parse(it)).defaultBlockState(), 3);
                            drain(a);
                            drain(obs);
                            a.gameMode.destroyBlock(above);
                        } else {
                            ItemStack st = new ItemStack(BuiltInRegistries.ITEM.getValue(Identifier.parse(it)));
                            a.setItemInHand(InteractionHand.MAIN_HAND, st);
                            drain(a);
                            drain(obs);
                            a.gameMode.useItemOn(a, level, st, InteractionHand.MAIN_HAND, new BlockHitResult(Vec3.atCenterOf(target).add(0, 0.5, 0), Direction.UP, target, false));
                        }
                        report(sc + "_" + it.substring(10), k, "obs", obs);
                        report(sc + "_" + it.substring(10), k, "self", a);
                        level.setBlock(above, net.minecraft.world.level.block.Blocks.AIR.defaultBlockState(), 3);
                        leave(a);
                    }
                }
            }
            case "step" -> {
                for (String t : new String[] {"minecraft:zombie", "minecraft:cow", "minecraft:spider", "minecraft:chicken", "minecraft:iron_golem"}) {
                    Mob z = mob(t, -10.5, -10.5);
                    stripAi(z);
                    drain(obs);
                    for (int i = 0; i < 40; i++) {
                        z.setOnGround(true);
                        z.move(MoverType.SELF, new Vec3(0.15, 0, 0));
                        report("step_" + t.substring(10), i, "obs", obs);
                    }
                    z.discard();
                }
                ServerPlayer a = actor(400);
                a.snapTo(-10.5, 101, 5.5, 0, 0);
                drain(obs);
                for (int i = 0; i < 40; i++) {
                    a.setOnGround(true);
                    a.move(MoverType.SELF, new Vec3(0.2, 0, 0));
                    report("step_player", i, "obs", obs);
                    report("step_player", i, "self", a);
                }
                leave(a);
            }
            case "pickup" -> {
                for (int i = 0; i < 4; i++) {
                    ServerPlayer a = actor(500 + i);
                    ItemEntity ie = new ItemEntity(level, 0.5, 101, 0.5, new ItemStack(BuiltInRegistries.ITEM.getValue(Identifier.parse("minecraft:stone"))));
                    ie.setNoPickUpDelay();
                    level.addFreshEntity(ie);
                    drain(obs);
                    drain(a);
                    ie.playerTouch(a);
                    report("pickup_item", i, "obs", obs);
                    report("pickup_item", i, "self", a);
                    ExperienceOrb orb = new ExperienceOrb(level, 0.5, 101, 0.5, 3);
                    level.addFreshEntity(orb);
                    a.takeXpDelay = 0;
                    drain(obs);
                    drain(a);
                    orb.playerTouch(a);
                    report("pickup_xp", i, "obs", obs);
                    report("pickup_xp", i, "self", a);
                    a.tickCount = 1000;
                    a.giveExperienceLevels(4);
                    drain(obs);
                    drain(a);
                    a.giveExperienceLevels(1);
                    report("levelup", i, "obs", obs);
                    report("levelup", i, "self", a);
                    leave(a);
                }
            }
            case "ambient" -> {
                List<Mob> zs = new ArrayList<>();
                for (int i = 0; i < 16; i++) {
                    Mob z = mob("minecraft:zombie", -14.5 + (i % 4) * 2, 6.5 + (i / 4) * 2);
                    stripAi(z);
                    zs.add(z);
                }
                drain(obs);
                Map<Integer, Integer> idx = new HashMap<>();
                for (int i = 0; i < zs.size(); i++) idx.put(zs.get(i).getId(), i);
                int[] last = new int[zs.size()];
                for (int t = 0; t < 4000; t++) {
                    for (Mob z : zs) z.tick();
                    for (Packet<?> pk : drain(obs)) {
                        if (pk instanceof ClientboundSoundEntityPacket q && idx.containsKey(q.getId())) {
                            int k = idx.get(q.getId());
                            Env.out.println("amb " + k + " " + t + " " + (t - last[k]) + " " + q.getSound().value().location() + " " + q.getSource().getName() + " " + q.getVolume() + " " + q.getPitch());
                            last[k] = t;
                        } else if (pk instanceof ClientboundSoundPacket q) {
                            Env.out.println("ambpos " + t + " " + q.getSound().value().location() + " " + q.getVolume() + " " + q.getPitch());
                        }
                    }
                }
                for (Mob z : zs) z.discard();
            }
            case "babies" -> {
                for (String t : new String[] {"minecraft:cow", "minecraft:pig", "minecraft:chicken", "minecraft:wolf", "minecraft:sheep", "minecraft:villager", "minecraft:horse"}) {
                    for (int k = 0; k < 3; k++) {
                        ServerPlayer a = actor(700 + k);
                        Mob z = mob(t, 0.5, 2.5);
                        if (z instanceof AgeableMob am) am.setAge(-24000);
                        drain(obs);
                        strength.setInt(a, 100);
                        a.attack(z);
                        report("babyhurt_" + t.substring(10), k, "obs", obs);
                        z.discard();
                        leave(a);
                    }
                }
            }
            case "variants" -> {
                for (String t : new String[] {"minecraft:cow", "minecraft:pig", "minecraft:chicken", "minecraft:cat", "minecraft:wolf", "minecraft:sheep", "minecraft:skeleton", "minecraft:creeper", "minecraft:spider", "minecraft:villager", "minecraft:horse", "minecraft:enderman"}) {
                    ServerPlayer a = actor(600);
                    Mob z = mob(t, 0.5, 2.5);
                    drain(obs);
                    strength.setInt(a, 100);
                    a.attack(z);
                    report("hurt_" + t.substring(10), 0, "obs", obs);
                    z.setHealth(0.5f);
                    z.damageCooldownTime = 0;
                    strength.setInt(a, 100);
                    a.attack(z);
                    report("die_" + t.substring(10), 0, "obs", obs);
                    z.discard();
                    leave(a);
                }
            }
        }
    }
}
