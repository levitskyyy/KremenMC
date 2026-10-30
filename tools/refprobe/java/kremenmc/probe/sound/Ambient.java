package kremenmc.probe.sound;

import java.util.*;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.network.protocol.Packet;
import net.minecraft.network.protocol.game.*;
import net.minecraft.resources.Identifier;
import net.minecraft.server.level.ServerPlayer;
import net.minecraft.server.level.TicketType;
import net.minecraft.world.entity.*;
import net.minecraft.world.level.ChunkPos;
import net.minecraft.world.level.GameType;

// Ambient lets the reference tick normally (sprinting) with stationary
// zombies (ambient sound intervals) and walkers pathing back and forth
// (step sounds), recording what an observer receives.
//
//	amb <mob> <tick> <name> <category> <volume> <pitch>
//	step <type> <tick> <kind> <name> <category> <volume> <pitch>
public final class Ambient {
    public static void main(String[] args) throws Exception {
        int ticks = Integer.parseInt(args.length > 0 ? args[0] : "6000");
        Env.boot();
        Env.run(() -> {
            Env.command("gamerule spawn_mobs false");
            Env.command("gamerule random_tick_speed 0");
            Env.command("gamerule advance_time false");
            Env.command("time set noon");
            for (int cx = -3; cx <= 3; cx++)
                for (int cz = -3; cz <= 3; cz++) Env.level.getChunkSource().addTicketWithRadius(TicketType.DRAGON, new ChunkPos(cx, cz), 2);
        });
        Env.waitTicks(20);
        Env.run(() -> Env.command("fill -40 100 -40 40 100 40 minecraft:stone"));
        Env.waitTicks(2);
        Map<Integer, String> names = new HashMap<>();
        List<Mob> walkers = new ArrayList<>();
        Env.run(() -> {
            Sounds.obs = Sounds.join("watcher");
            Sounds.obs.snapTo(0.5, 101, 0.5, 0, 0);
            Sounds.obs.setGameMode(GameType.SPECTATOR);
            for (int i = 0; i < 16; i++) {
                Mob z = Sounds.mob("minecraft:zombie", -30.5 + (i % 4) * 3, -30.5 + (i / 4) * 3);
                Sounds.stripAi(z);
                z.setPersistenceRequired();
                names.put(z.getId(), "zombie" + i);
            }
            String[] ws = {"minecraft:zombie", "minecraft:cow", "minecraft:spider", "minecraft:chicken", "minecraft:iron_golem", "minecraft:pig", "minecraft:sheep", "minecraft:skeleton", "minecraft:creeper", "minecraft:wolf"};
            for (int i = 0; i < ws.length; i++) {
                Mob m = Sounds.mob(ws[i], -20.5, -10.5 + i * 3);
                Sounds.stripAi(m);
                m.setPersistenceRequired();
                walkers.add(m);
                names.put(m.getId(), "walk_" + ws[i].substring(10));
            }
        });
        Env.run(() -> Env.command("tick sprint " + ticks));
        int start = Env.call(() -> Env.server.getTickCount());
        int now = start;
        while (now < start + ticks) {
            Thread.sleep(5);
            now = Env.call(() -> {
                int t = Env.server.getTickCount() - start;
                for (Mob m : walkers) {
                    if (m.getNavigation().isDone()) {
                        double tx = m.getX() < 0 ? 20.5 : -20.5;
                        m.getNavigation().moveTo(tx, 101, m.getZ(), 1.0);
                    }
                }
                for (Packet<?> pk : Sounds.drain(Sounds.obs)) {
                    String s = null;
                    int id = -1;
                    if (pk instanceof ClientboundSoundEntityPacket q) {
                        id = q.getId();
                        s = "ent " + q.getSound().value().location() + " " + q.getSource().getName() + " " + q.getVolume() + " " + q.getPitch();
                    } else if (pk instanceof ClientboundSoundPacket q) {
                        s = "pos " + q.getSound().value().location() + " " + q.getSource().getName() + " " + q.getVolume() + " " + q.getPitch() + " " + q.getX() + "," + q.getZ();
                    }
                    if (s != null) Env.out.println("snd " + (id >= 0 ? names.getOrDefault(id, "id" + id) : "-") + " " + t + " " + s);
                }
                return Env.server.getTickCount();
            });
        }
        Env.exit();
    }
}
