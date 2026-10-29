package kremenmc.probe.dig;

import java.nio.file.*;
import java.util.*;
import com.mojang.authlib.GameProfile;
import com.mojang.brigadier.StringReader;
import io.netty.channel.embedded.EmbeddedChannel;
import net.minecraft.commands.arguments.blocks.BlockStateParser;
import net.minecraft.commands.arguments.item.ItemParser;
import net.minecraft.core.BlockPos;
import net.minecraft.core.Direction;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.core.registries.Registries;
import net.minecraft.network.Connection;
import net.minecraft.network.protocol.Packet;
import net.minecraft.network.protocol.PacketFlow;
import net.minecraft.network.protocol.game.*;
import net.minecraft.server.level.ClientInformation;
import net.minecraft.server.level.ServerLevel;
import net.minecraft.server.level.ServerPlayer;
import net.minecraft.server.network.CommonListenerCookie;
import net.minecraft.world.effect.MobEffectInstance;
import net.minecraft.world.effect.MobEffects;
import net.minecraft.world.entity.EquipmentSlot;
import net.minecraft.world.entity.item.ItemEntity;
import net.minecraft.world.item.ItemStack;
import net.minecraft.world.level.GameType;
import net.minecraft.world.level.block.Block;
import net.minecraft.world.level.block.Blocks;
import net.minecraft.world.level.block.state.BlockState;
import net.minecraft.world.phys.AABB;

/**
 * Block breaking oracle. Input lines:
 *   blocks                                  -> per block: hardness bits, requiresCorrectToolForDrops
 *   speed|id|state|tool|helmet|haste|fatigue|conduit|ground|water -> getDestroyProgress bits, hasCorrectToolForDrops
 *   seq|id|state|tool|gm|ops                -> trace of ServerPlayerGameMode for ops S/F/A/Tn
 */
public final class Dig {
    static ServerLevel level;
    static ItemParser items;
    static final BlockPos FEET = new BlockPos(0, 100, 0);
    static final BlockPos TARGET = new BlockPos(1, 100, 0);

    public static void main(String[] args) throws Exception {
        var server = Ref.boot();
        level = server.overworld();
        items = new ItemParser(server.registryAccess());
        List<String> lines = Files.readAllLines(Path.of(args[0]));
        boolean dig = args.length > 1 && args[1].equals("digest");
        Map<String, List<String>> groups = new LinkedHashMap<>();
        server.submit(() -> {
            level.getChunk(0, 0);
            ServerPlayer p = join(server, "digger");
            ServerPlayer obs = join(server, "watcher");
            int k = 0;
            for (String line : lines) {
                String[] f = line.strip().split("\\|", -1);
                try {
                    switch (f[0]) {
                        case "blocks" -> blocks();
                        case "speed" -> {
                            String r = speed(p, f);
                            if (dig && f[1].startsWith("b")) groups.computeIfAbsent(f[3], x -> new ArrayList<>()).add(r);
                            else Ref.out.println(r);
                        }
                        case "seq" -> {
                            ServerPlayer q = join(server, "d" + (k++));
                            seq(q, obs, f);
                            server.getPlayerList().remove(q);
                            chans.remove(q);
                        }
                        default -> {}
                    }
                } catch (Throwable e) {
                    Ref.out.println("error " + line + " " + e);
                }
            }
            for (var e : groups.entrySet()) {
                var crc = new java.util.zip.CRC32();
                crc.update(String.join("\n", e.getValue()).getBytes(java.nio.charset.StandardCharsets.UTF_8));
                Ref.out.println("digest " + e.getKey() + " " + String.format("%08x", crc.getValue()) + " " + e.getValue().size());
            }
            return null;
        }).get();
        Ref.exit();
    }

    static final Map<ServerPlayer, EmbeddedChannel> chans = new HashMap<>();

    static ServerPlayer join(net.minecraft.server.MinecraftServer server, String name) {
        var prof = new GameProfile(UUID.randomUUID(), name);
        Connection conn = new Connection(PacketFlow.SERVERBOUND);
        EmbeddedChannel ch = new EmbeddedChannel(conn);
        ServerPlayer p = new ServerPlayer(server, level, prof, ClientInformation.createDefault());
        server.getPlayerList().placeNewPlayer(conn, p, CommonListenerCookie.createInitial(prof, false));
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

    static void blocks() {
        for (Block b : BuiltInRegistries.BLOCK) {
            Set<Float> speeds = new TreeSet<>();
            Set<Boolean> req = new TreeSet<>();
            for (BlockState s : b.getStateDefinition().getPossibleStates()) {
                speeds.add(s.getDestroySpeed(level, TARGET));
                req.add(s.requiresCorrectToolForDrops());
            }
            Ref.out.println("block " + BuiltInRegistries.BLOCK.getKey(b).getPath() + " " + speeds + " " + req);
        }
    }

    static BlockState state(String s) throws Exception {
        return BlockStateParser.parseForBlock(level.holderLookup(Registries.BLOCK), s, false).blockState();
    }

    static ItemStack stack(String spec) throws Exception {
        if (spec.equals("-")) return ItemStack.EMPTY;
        var in = items.parse(new StringReader(spec));
        return new ItemStack(in.item(), 1, in.components());
    }

    static void reset(ServerPlayer p) {
        p.removeAllEffects();
        for (int x = -2; x <= 3; x++)
            for (int y = 98; y <= 103; y++)
                for (int z = -2; z <= 2; z++) level.setBlock(new BlockPos(x, y, z), y == 99 ? Blocks.STONE.defaultBlockState() : Blocks.AIR.defaultBlockState(), 2 | 16);
        p.snapTo(0.5, 100, 0.5, 0, 0);
        p.setDeltaMovement(0, 0, 0);
        p.setOnGround(true);
    }

    /** Applies equipment attribute modifiers now (LivingEntity does it on its next tick). */
    static void equip(ServerPlayer p) throws Exception {
        var m = net.minecraft.world.entity.LivingEntity.class.getDeclaredMethod("detectEquipmentUpdates");
        m.setAccessible(true);
        m.invoke(p);
    }

    static String speed(ServerPlayer p, String[] f) throws Exception {
        p.setGameMode(GameType.SURVIVAL);
        reset(p);
        BlockState st = state(f[2]);
        level.setBlock(TARGET, st, 2 | 16);
        p.setItemSlot(EquipmentSlot.MAINHAND, stack(f[3]));
        p.setItemSlot(EquipmentSlot.HEAD, stack(f[4]));
        if (!f[5].equals("-")) p.addEffect(new MobEffectInstance(MobEffects.HASTE, 1000, Integer.parseInt(f[5])));
        if (!f[6].equals("-")) p.addEffect(new MobEffectInstance(MobEffects.MINING_FATIGUE, 1000, Integer.parseInt(f[6])));
        if (!f[7].equals("-")) p.addEffect(new MobEffectInstance(MobEffects.CONDUIT_POWER, 1000, Integer.parseInt(f[7])));
        if (f[9].equals("1")) {
            level.setBlock(FEET, Blocks.WATER.defaultBlockState(), 2 | 16);
            level.setBlock(FEET.above(), Blocks.WATER.defaultBlockState(), 2 | 16);
        }
        equip(p);
        var m = net.minecraft.world.entity.Entity.class.getDeclaredMethod("updateFluidInteraction");
        m.setAccessible(true);
        m.invoke(p);
        p.setOnGround(f[8].equals("1"));
        float prog = st.getDestroyProgress(p, level, TARGET);
        return "speed " + f[1] + " " + Integer.toHexString(Float.floatToRawIntBits(prog)) + " " + p.hasCorrectToolForDrops(st) + " eye=" + p.isEyeInFluid(net.minecraft.tags.FluidTags.WATER);
    }

    static String gmState(ServerPlayer p) throws Exception {
        StringBuilder b = new StringBuilder("{");
        for (String n : new String[]{"gameTicks", "destroyProgressStart", "isDestroyingBlock", "hasDelayedDestroy", "delayedTickStart", "lastSentState"}) {
            var fl = net.minecraft.server.level.ServerPlayerGameMode.class.getDeclaredField(n);
            fl.setAccessible(true);
            b.append(n.charAt(0)).append(n.length()).append('=').append(fl.get(p.gameMode)).append(',');
        }
        return b.append(p.onGround()).append('}').toString();
    }

    static void seq(ServerPlayer p, ServerPlayer obs, String[] f) throws Exception {
        p.setGameMode(f[4].equals("c") ? GameType.CREATIVE : f[4].equals("a") ? GameType.ADVENTURE : GameType.SURVIVAL);
        reset(p);
        obs.snapTo(3.5, 100, 2.5, 0, 0);
        BlockState st = state(f[2]);
        level.setBlock(TARGET, st, 2 | 16);
        p.setItemSlot(EquipmentSlot.MAINHAND, stack(f[3]));
        p.getInventory().setSelectedSlot(0);
        equip(p);
        drain(p);
        drain(obs);
        p.setOnGround(true);
        StringBuilder sb = new StringBuilder("seq " + f[1] + " g=" + p.onGround());
        int seqNo = 1;
        Direction face = f.length > 6 && !f[6].isEmpty() ? Direction.from3DDataValue(Integer.parseInt(f[6])) : Direction.UP;
        for (String op : f[5].split(" ")) {
            int n = 1;
            char c = op.charAt(0);
            if (c == 'T') n = Integer.parseInt(op.substring(1));
            for (int i = 0; i < n; i++) {
                switch (c) {
                    case 'S' -> p.connection.handlePlayerAction(new ServerboundPlayerActionPacket(ServerboundPlayerActionPacket.Action.START_DESTROY_BLOCK, TARGET, face, seqNo++));
                    case 'F' -> p.connection.handlePlayerAction(new ServerboundPlayerActionPacket(ServerboundPlayerActionPacket.Action.STOP_DESTROY_BLOCK, TARGET, face, seqNo++));
                    case 'A' -> p.connection.handlePlayerAction(new ServerboundPlayerActionPacket(ServerboundPlayerActionPacket.Action.ABORT_DESTROY_BLOCK, TARGET, face, seqNo++));
                    case 'C' -> p.connection.handlePlayerAction(new ServerboundPlayerActionPacket(ServerboundPlayerActionPacket.Action.CHANGE_DESTROY_DIRECTION, TARGET, Direction.from3DDataValue(Integer.parseInt(op.substring(1))), seqNo++));
                    case 'T' -> p.gameMode.tick();
                    default -> {}
                }
            }
            sb.append(' ').append(op).append('[');
            if (System.getenv("DIGSTATE") != null) sb.append(gmState(p)).append(' ');
            sb.append(level.getBlockState(TARGET).isAir() ? "air" : "blk");
            for (Packet<?> pk : drain(p)) {
                if (pk instanceof ClientboundBlockUpdatePacket u && u.getPos().equals(TARGET)) sb.append(" upd:").append(u.getBlockState().isAir() ? "air" : "blk");
                if (pk instanceof ClientboundBlockChangedAckPacket a) sb.append(" ack").append(a.sequence());
                if (pk instanceof ClientboundBlockDestructionPacket d) sb.append(" selfd").append(d.getProgress());
                if (pk instanceof ClientboundLevelEventPacket e && e.getPos().equals(TARGET)) sb.append(" selfev").append(e.getType());
            }
            for (Packet<?> pk : drain(obs)) {
                if (pk instanceof ClientboundBlockDestructionPacket d) sb.append(" d").append(d.getProgress()).append(d.getId() == p.getId() ? "" : "?");
                if (pk instanceof ClientboundLevelEventPacket e && e.getPos().equals(TARGET)) sb.append(" ev").append(e.getType()).append(':').append(e.getData() == Block.getId(st) ? "st" : e.getData() + (e.isGlobalEvent() ? "g" : ""));
            }
            sb.append(']');
        }
        int drops = 0;
        for (ItemEntity e : level.getEntitiesOfClass(ItemEntity.class, new AABB(-5, 90, -5, 6, 110, 6))) {
            drops += e.getItem().getCount();
            e.discard();
        }
        sb.append(" drops=").append(drops);
        sb.append(" dmg=").append(p.getMainHandItem().getDamageValue());
        Ref.out.println(sb);
    }
}
