package kremenmc.probe.explosion;

import io.netty.buffer.Unpooled;
import java.lang.reflect.Field;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.*;
import net.minecraft.core.BlockPos;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.network.RegistryFriendlyByteBuf;
import net.minecraft.network.protocol.Packet;
import net.minecraft.network.protocol.game.ClientboundExplodePacket;
import net.minecraft.resources.Identifier;
import net.minecraft.world.entity.Entity;
import net.minecraft.world.entity.EntitySpawnReason;
import net.minecraft.world.entity.EntityType;
import net.minecraft.world.entity.LivingEntity;
import net.minecraft.world.entity.Mob;
import net.minecraft.world.entity.item.ItemEntity;
import net.minecraft.world.entity.item.PrimedTnt;
import net.minecraft.world.level.Level;
import net.minecraft.world.level.block.Block;
import net.minecraft.world.level.block.state.BlockState;
import net.minecraft.world.level.levelgen.LegacyRandomSource;
import net.minecraft.world.phys.BlockHitResult;

// Explode runs explosion scenes from a script against the reference and
// prints the outcome: the level random call sequence, changed blocks, the
// state of scene entities, entities created by the explosion and the
// explode packets each fake player received. Script lines:
//
//	scene <name>                      start a scene (clears the area and entities)
//	area x0 y0 z0 x1 y1 z1            region diffed and cleared
//	cmd <command>                     run a command
//	set x y z <state>                 setblock
//	mob <type> x y z                  spawn a no-AI entity
//	player x y z [gamemode]           add a fake player
//	explode x y z power interaction fire seed [tnt]
//	tntprime x y z seed               TntBlock.prime at the block
//	tnttick fuse x y z vx vy vz ticks seed
//	seen x y z / seenrand seed n      getSeenPercent for scene entities / random boxes
//	clip x0 y0 z0 x1 y1 z1            a collider ClipContext ray
//	firestate x y z                   BaseFireBlock.getState and canBePlacedAt
//	use item x y z face sneak yaw seed  a survival player uses item on a block face
//	tntmeta fuse                      PrimedTnt non-default entity data
//
// Every op is echoed as "op <line>"; ops that read blocks first print the
// area as "pre" runs so the Go side can rebuild it.
public final class Explode {
    static int x0, y0, z0, x1, y1, z1;
    static final List<Entity> ents = new ArrayList<>();
    static final List<FakePlayer> players = new ArrayList<>();
    static Map<Long, String> before = new HashMap<>();
    static Set<Integer> known = new HashSet<>();
    static List<String> log = new ArrayList<>();

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
            Env.command("difficulty normal");
        });
        Env.waitTicks(5);
        o.println("worldseed " + Env.level.getSeed());
        Env.run(() -> Env.server.tickRateManager().setFrozen(true));
        Env.waitTicks(2);
        for (String raw : lines) {
            String line = raw.strip();
            if (line.isEmpty() || line.startsWith("#")) continue;
            if (line.startsWith("wait ")) {
                Env.waitTicks(Integer.parseInt(line.substring(5)));
                continue;
            }
            o.println("op " + line);
            String res;
            try {
                res = Env.call(() -> op(line));
            } catch (Exception e) {
                o.println("error " + line + ": " + e);
                var c = e.getCause() != null ? e.getCause() : e;
                for (var st : c.getStackTrace()) o.println("  at " + st);
                Env.exit();
                return;
            }
            if (!res.isEmpty()) o.print(res);
            for (String f : Env.failures) o.println("warn " + f);
            Env.failures.clear();
        }
        Env.exit();
    }

    static String op(String line) throws Exception {
        String[] f = line.split("\\s+");
        var level = Env.level;
        StringBuilder sb = new StringBuilder();
        switch (f[0]) {
            case "scene" -> {
                for (Entity e : ents) e.discard();
                for (FakePlayer p : players) level.removePlayerImmediately(p, Entity.RemovalReason.DISCARDED);
                List<Entity> all = new ArrayList<>();
                for (Entity e : level.getAllEntities()) if (e != null && !(e instanceof net.minecraft.world.entity.player.Player)) all.add(e);
                for (Entity e : all) e.discard();
                ents.clear();
                players.clear();
                sb.append("scene ").append(f[1]).append('\n');
            }
            case "area" -> {
                x0 = i(f[1]); y0 = i(f[2]); z0 = i(f[3]); x1 = i(f[4]); y1 = i(f[5]); z1 = i(f[6]);
                Env.command(String.format("forceload add %d %d %d %d", x0, z0, x1, z1));
                Env.command(String.format("fill %d %d %d %d %d %d air", x0, y0, z0, x1, y1, z1));
            }
            case "cmd" -> Env.command(line.substring(4));
            case "set" -> Env.command("setblock " + f[1] + " " + f[2] + " " + f[3] + " " + f[4]);
            case "fill" -> Env.command("fill " + String.join(" ", Arrays.copyOfRange(f, 1, f.length)));
            case "mob" -> {
                EntityType<?> t = BuiltInRegistries.ENTITY_TYPE.getValue(Identifier.parse(f[1]));
                Entity e = t.create(level, EntitySpawnReason.COMMAND);
                e.snapTo(d(f[2]), d(f[3]), d(f[4]), 0, 0);
                if (e instanceof Mob m) m.setNoAi(true);
                if (f.length > 6 && f[5].equals("hp") && e instanceof LivingEntity l) {
                    l.getAttribute(net.minecraft.world.entity.ai.attributes.Attributes.MAX_HEALTH).setBaseValue(d(f[6]));
                    l.setHealth(Float.parseFloat(f[6]));
                }
                if (!level.addFreshEntity(e)) sb.append("warn add failed ").append(line).append('\n');
                ents.add(e);
            }
            case "player" -> {
                FakePlayer p = new FakePlayer(level, players.size());
                p.snapTo(d(f[1]), d(f[2]), d(f[3]), 0, 0);
                level.addNewPlayer(p);
                if (f.length > 4) p.setGameMode(net.minecraft.world.level.GameType.byName(f[4]));
                p.sent();
                players.add(p);
                sb.append("gm ").append(p.gameMode()).append('\n');
                ents.add(p);
            }
            case "seen" -> {
                snapshot();
                pre(sb);
                var c = new net.minecraft.world.phys.Vec3(d(f[1]), d(f[2]), d(f[3]));
                for (int k = 0; k < ents.size(); k++)
                    sb.append("seen ").append(k).append(' ').append(net.minecraft.world.level.ServerExplosion.getSeenPercent(c, ents.get(k))).append('\n');
            }
            case "seenrand" -> {
                // Random boxes and centres inside the area against the current blocks.
                snapshot();
                pre(sb);
                var rnd = new java.util.Random(Long.parseLong(f[1]));
                Entity probe = new net.minecraft.world.entity.decoration.ArmorStand(level, 0, 0, 0);
                for (int k = 0; k < i(f[2]); k++) {
                    double cx = x0 + rnd.nextDouble() * (x1 - x0 + 1), cy = y0 + rnd.nextDouble() * (y1 - y0 + 1), cz = z0 + rnd.nextDouble() * (z1 - z0 + 1);
                    double w = rnd.nextInt(4) == 0 ? rnd.nextInt(3) * 0.5 + 0.5 : 0.2 + rnd.nextDouble() * 1.8;
                    double h = rnd.nextInt(4) == 0 ? rnd.nextInt(4) * 0.5 + 0.5 : 0.2 + rnd.nextDouble() * 2.5;
                    double bx = x0 + rnd.nextDouble() * (x1 - x0 + 1), by = y0 + rnd.nextDouble() * (y1 - y0 + 1), bz = z0 + rnd.nextDouble() * (z1 - z0 + 1);
                    if (rnd.nextInt(3) == 0) { bx = Math.floor(bx) + 0.5; by = Math.floor(by); bz = Math.floor(bz) + 0.5; }
                    var box = new net.minecraft.world.phys.AABB(bx - w / 2, by, bz - w / 2, bx + w / 2, by + h, bz + w / 2);
                    probe.setBoundingBox(box);
                    float seen = net.minecraft.world.level.ServerExplosion.getSeenPercent(new net.minecraft.world.phys.Vec3(cx, cy, cz), probe);
                    sb.append("sr ").append(cx).append(' ').append(cy).append(' ').append(cz).append(' ').append(box.minX).append(' ').append(box.minY).append(' ').append(box.minZ)
                        .append(' ').append(box.maxX).append(' ').append(box.maxY).append(' ').append(box.maxZ).append(' ').append(seen).append('\n');
                }
            }
            case "firestate" -> {
                // firestate x y z: BaseFireBlock.getState and canBePlacedAt for each horizontal facing.
                snapshot();
                pre(sb);
                BlockPos bp = new BlockPos(i(f[1]), i(f[2]), i(f[3]));
                sb.append("fire ").append(f[1]).append(' ').append(f[2]).append(' ').append(f[3]).append(' ').append(str(net.minecraft.world.level.block.BaseFireBlock.getState(level, bp)));
                for (var d : net.minecraft.core.Direction.Plane.HORIZONTAL) sb.append(' ').append(net.minecraft.world.level.block.BaseFireBlock.canBePlacedAt(level, bp, d) ? 1 : 0);
                sb.append('\n');
            }
            case "use" -> {
                // use <item> x y z face sneak yaw seed: survival player uses the held stack on a block face.
                snapshot();
                FakePlayer p = new FakePlayer(level, 90 + players.size());
                BlockPos bp = new BlockPos(i(f[2]), i(f[3]), i(f[4]));
                var face = net.minecraft.core.Direction.byName(f[5]);
                p.snapTo(bp.getX() + 0.5 + face.getStepX() * 2, bp.getY() + face.getStepY() * 2 - (face == net.minecraft.core.Direction.DOWN ? 1 : 0), bp.getZ() + 0.5 + face.getStepZ() * 2, Float.parseFloat(f[7]), 0);
                level.addNewPlayer(p);
                p.setGameMode(net.minecraft.world.level.GameType.SURVIVAL);
                p.setShiftKeyDown(f[6].equals("1"));
                var stack = new net.minecraft.world.item.ItemStack(BuiltInRegistries.ITEM.getValue(Identifier.parse(f[1])), 2);
                p.setItemInHand(net.minecraft.world.InteractionHand.MAIN_HAND, stack);
                p.sent();
                Rec r = install(Long.parseLong(f[8]));
                r.on = true;
                var hit = new BlockHitResult(net.minecraft.world.phys.Vec3.atCenterOf(bp).add(face.getStepX() * 0.5, face.getStepY() * 0.5, face.getStepZ() * 0.5), face, bp, false);
                var res = p.gameMode.useItemOn(p, level, stack, net.minecraft.world.InteractionHand.MAIN_HAND, hit);
                r.on = false;
                var held = p.getItemInHand(net.minecraft.world.InteractionHand.MAIN_HAND);
                sb.append("used ").append(res).append(" held ").append(BuiltInRegistries.ITEM.getKey(held.getItem()).getPath()).append(' ').append(held.getCount()).append(" damage ").append(held.getDamageValue()).append('\n');
                StringBuilder ps = new StringBuilder();
                for (Packet<?> pk : p.sent()) ps.append(' ').append(pk.getClass().getSimpleName());
                sb.append("upk").append(ps).append('\n');
                report(sb);
                level.removePlayerImmediately(p, Entity.RemovalReason.DISCARDED);
            }
            case "clip" -> {
                var from = new net.minecraft.world.phys.Vec3(d(f[1]), d(f[2]), d(f[3]));
                var to = new net.minecraft.world.phys.Vec3(d(f[4]), d(f[5]), d(f[6]));
                Entity probe = new net.minecraft.world.entity.decoration.ArmorStand(level, 0, 0, 0);
                var h = level.clip(new net.minecraft.world.level.ClipContext(from, to, net.minecraft.world.level.ClipContext.Block.COLLIDER, net.minecraft.world.level.ClipContext.Fluid.NONE, probe));
                sb.append("clipped ").append(h.getType()).append(' ').append(h.getBlockPos().toShortString().replace(" ", "")).append(' ').append(h.getLocation()).append('\n');
            }
            case "tntmeta" -> {
                PrimedTnt t = new PrimedTnt(level, 0.5, 100, 0.5, null);
                t.setFuse(i(f[1]));
                var vals = t.getEntityData().getNonDefaultValues();
                if (vals == null) {
                    sb.append("meta ").append(f[1]).append(" -\n");
                    break;
                }
                var pk = new net.minecraft.network.protocol.game.ClientboundSetEntityDataPacket(t.getId(), vals);
                var buf = new RegistryFriendlyByteBuf(Unpooled.buffer(), Env.server.registryAccess());
                net.minecraft.network.protocol.game.ClientboundSetEntityDataPacket.STREAM_CODEC.encode(buf, pk);
                byte[] a = new byte[buf.readableBytes()];
                buf.getBytes(0, a);
                // Strip the entity id VarInt.
                int k = 0;
                while ((a[k] & 0x80) != 0) k++;
                sb.append("meta ").append(f[1]).append(' ').append(HexFormat.of().formatHex(Arrays.copyOfRange(a, k + 1, a.length))).append('\n');
            }
            case "explode" -> {
                snapshot();
                // The source is built first: its constructor draws from the level random.
                Entity src = null;
                if (f.length > 8 && f[8].equals("tnt")) src = new PrimedTnt(level, d(f[1]), d(f[2]), d(f[3]), null);
                Rec r = install(Long.parseLong(f[7]));
                r.on = true;
                level.explode(src, d(f[1]), d(f[2]), d(f[3]), Float.parseFloat(f[4]), f[6].equals("1"), Level.ExplosionInteraction.valueOf(f[5]));
                r.on = false;
                report(sb);
            }
            case "tntprime" -> {
                snapshot();
                Rec r = install(Long.parseLong(f[4]));
                r.on = true;
                boolean ok = net.minecraft.world.level.block.TntBlock.prime(level, new BlockPos(i(f[1]), i(f[2]), i(f[3])));
                r.on = false;
                sb.append("primed ").append(ok).append('\n');
                report(sb);
            }
            case "tnttick" -> {
                snapshot();
                Rec r = install(Long.parseLong(f[9]));
                @SuppressWarnings("unchecked") var tt = (EntityType<PrimedTnt>) BuiltInRegistries.ENTITY_TYPE.getValue(Identifier.parse("tnt"));
                PrimedTnt t = new PrimedTnt(tt, level);
                t.setFuse(i(f[1]));
                t.setPos(d(f[2]), d(f[3]), d(f[4]));
                t.setDeltaMovement(d(f[5]), d(f[6]), d(f[7]));
                level.addFreshEntity(t);
                known.add(t.getId());
                r.on = true;
                int n = i(f[8]);
                for (int k = 0; k < n && !t.isRemoved(); k++) {
                    t.tick();
                    var v = t.getDeltaMovement();
                    sb.append("t ").append(k).append(' ').append(t.getX()).append(' ').append(t.getY()).append(' ').append(t.getZ())
                        .append(' ').append(v.x).append(' ').append(v.y).append(' ').append(v.z).append(' ').append(t.onGround()).append(' ').append(t.getFuse()).append('\n');
                }
                r.on = false;
                report(sb);
            }
            default -> throw new IllegalArgumentException(line);
        }
        return sb.toString();
    }

    static Rec install(long seed) throws Exception {
        log = new ArrayList<>();
        Rec r = new Rec(new LegacyRandomSource(seed), "L", log);
        r.on = false;
        Field rf = Level.class.getDeclaredField("random");
        rf.setAccessible(true);
        rf.set(Env.level, r);
        return r;
    }

    static void snapshot() {
        before.clear();
        known.clear();
        for (int x = x0; x <= x1; x++)
            for (int y = y0; y <= y1; y++)
                for (int z = z0; z <= z1; z++) before.put(BlockPos.asLong(x, y, z), str(Env.level.getBlockState(new BlockPos(x, y, z))));
        for (Entity e : Env.level.getAllEntities()) if (e != null) known.add(e.getId());
        for (FakePlayer p : players) p.sent();
    }

    static String str(BlockState s) {
        String full = s.toString();
        // Block{minecraft:stone}[a=b] -> minecraft:stone[a=b]
        return full.replaceFirst("^Block\\{([^}]*)\\}", "$1");
    }

    // pre prints the non-air blocks of the area as z runs: pre x y z0 z1 state.
    static void pre(StringBuilder sb) {
        for (int x = x0; x <= x1; x++)
            for (int y = y0; y <= y1; y++)
                for (int z = z0; z <= z1;) {
                    String st = before.get(BlockPos.asLong(x, y, z));
                    int e = z;
                    while (e + 1 <= z1 && st.equals(before.get(BlockPos.asLong(x, y, e + 1)))) e++;
                    if (!st.equals("minecraft:air")) sb.append("pre ").append(x).append(' ').append(y).append(' ').append(z).append(' ').append(e).append(' ').append(st).append('\n');
                    z = e + 1;
                }
    }

    static void report(StringBuilder sb) throws Exception {
        StringBuilder head = new StringBuilder();
        pre(head);
        sb.insert(0, head);
        sb.append("rand ").append(compress(log)).append('\n');
        for (int x = x0; x <= x1; x++)
            for (int y = y0; y <= y1; y++)
                for (int z = z0; z <= z1; z++) {
                    String now = str(Env.level.getBlockState(new BlockPos(x, y, z)));
                    if (!now.equals(before.get(BlockPos.asLong(x, y, z)))) sb.append("block ").append(x).append(' ').append(y).append(' ').append(z).append(' ').append(now).append('\n');
                }
        for (int k = 0; k < ents.size(); k++) {
            Entity e = ents.get(k);
            sb.append("ent ").append(k).append(' ').append(ent(e)).append('\n');
        }
        List<Entity> fresh = new ArrayList<>();
        for (Entity e : Env.level.getAllEntities()) if (e != null && !known.contains(e.getId())) fresh.add(e);
        fresh.sort(Comparator.comparingInt(Entity::getId));
        for (Entity e : fresh) sb.append("new ").append(ent(e)).append('\n');
        for (int k = 0; k < players.size(); k++) {
            for (Packet<?> p : players.get(k).sent()) {
                if (p instanceof ClientboundExplodePacket ep) {
                    var buf = new RegistryFriendlyByteBuf(Unpooled.buffer(), Env.server.registryAccess());
                    ClientboundExplodePacket.STREAM_CODEC.encode(buf, ep);
                    byte[] a = new byte[buf.readableBytes()];
                    buf.getBytes(0, a);
                    sb.append("pkt ").append(k).append(" explode ").append(HexFormat.of().formatHex(a)).append('\n');
                } else {
                    sb.append("pkt ").append(k).append(' ').append(p.getClass().getSimpleName()).append('\n');
                }
            }
        }
    }

    static String ent(Entity e) {
        var v = e.getDeltaMovement();
        StringBuilder sb = new StringBuilder();
        sb.append(BuiltInRegistries.ENTITY_TYPE.getKey(e.getType()).getPath()).append(' ').append(e.getX()).append(' ').append(e.getY()).append(' ').append(e.getZ())
            .append(' ').append(v.x).append(' ').append(v.y).append(' ').append(v.z);
        if (e instanceof LivingEntity l) sb.append(" hp ").append(l.getHealth());
        if (e instanceof ItemEntity it) sb.append(" item ").append(BuiltInRegistries.ITEM.getKey(it.getItem().getItem()).getPath()).append(' ').append(it.getItem().getCount());
        if (e instanceof PrimedTnt t) sb.append(" fuse ").append(t.getFuse());
        if (e.isRemoved()) sb.append(" removed");
        return sb.toString();
    }

    // compress turns the call log into tokens: F nextFloat, D nextDouble,
    // In nextInt(n)=v, others verbatim; runs of equal value-free tokens are
    // written as tok*count.
    static String compress(List<String> log) {
        List<String> toks = new ArrayList<>();
        for (String l : log) {
            String s = l.substring(4); // "r L "
            String t;
            if (s.startsWith("nextFloat")) t = "F";
            else if (s.startsWith("nextDouble")) t = "D";
            else if (s.startsWith("nextIntn ")) {
                String[] p = s.split(" ");
                t = "I" + p[1] + "=" + p[3];
            } else t = s.replace(' ', '_');
            toks.add(t);
        }
        StringBuilder sb = new StringBuilder();
        for (int k = 0; k < toks.size();) {
            int j = k;
            while (j < toks.size() && toks.get(j).equals(toks.get(k))) j++;
            if (sb.length() > 0) sb.append(' ');
            sb.append(toks.get(k));
            if (j - k > 1) sb.append('*').append(j - k);
            k = j;
        }
        return sb.length() == 0 ? "-" : sb.toString();
    }

    static int i(String s) { return Integer.parseInt(s); }
    static double d(String s) { return Double.parseDouble(s); }
}
