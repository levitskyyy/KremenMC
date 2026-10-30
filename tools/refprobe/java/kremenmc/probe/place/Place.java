package kremenmc.probe.place;

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
import net.minecraft.network.Connection;
import net.minecraft.network.protocol.Packet;
import net.minecraft.network.protocol.PacketFlow;
import net.minecraft.network.protocol.game.*;
import net.minecraft.server.level.ClientInformation;
import net.minecraft.server.level.ServerLevel;
import net.minecraft.server.level.ServerPlayer;
import net.minecraft.server.network.CommonListenerCookie;
import net.minecraft.world.InteractionHand;
import net.minecraft.world.item.ItemStack;
import net.minecraft.world.level.GameType;
import net.minecraft.world.level.block.Blocks;
import net.minecraft.world.phys.BlockHitResult;
import net.minecraft.world.phys.Vec3;

/**
 * Block placement / shape update oracle. Runs a script of ops against the
 * real ServerPlayerGameMode.useItemOn and Level.setBlock and prints the
 * resulting states and the packets a watching player received:
 *   reset | set|x|y|z|state | place|x|y|z|face|cx|cy|cz|yaw|pitch|sneak|item
 *   get|x|y|z | use|x|y|z|sneak | close | break|x|y|z | wait|ms | drain
 */
public final class Place {
    static ServerLevel level;
    static final Map<ServerPlayer, EmbeddedChannel> chans = new HashMap<>();

    public static void main(String[] args) throws Exception {
        var server = Ref.boot();
        level = server.overworld();
        ItemParser items = new ItemParser(server.registryAccess());
        List<String> lines = Files.readAllLines(Path.of(args[0]));
        ServerPlayer[] ps = new ServerPlayer[2];
        server.submit(() -> {
            for (int cx = -1; cx <= 1; cx++) for (int cz = -1; cz <= 1; cz++) level.getChunk(cx, cz);
            ps[0] = join(server, "placer");
            ps[1] = join(server, "watcher");
            for (ServerPlayer p : ps) {
                p.setGameMode(GameType.CREATIVE);
                p.setPos(4.5, 112, 4.5);
            }
            return null;
        }).get();
        ServerPlayer p = ps[0], obs = ps[1];
        for (String line : lines) {
            String[] f = line.strip().split("\\|", -1);
            if (f[0].isEmpty() || f[0].startsWith("#")) continue;
            if (f[0].equals("wait")) {
                Thread.sleep(Long.parseLong(f[1]));
                continue;
            }
            server.submit(() -> {
                try {
                    op(p, obs, items, f);
                } catch (Throwable e) {
                    Ref.out.println("error " + line + " " + e);
                }
                return null;
            }).get();
        }
        Ref.exit();
    }

    static BlockPos pos(String[] f, int i) {
        return new BlockPos(Integer.parseInt(f[i]), Integer.parseInt(f[i + 1]), Integer.parseInt(f[i + 2]));
    }

    static void op(ServerPlayer p, ServerPlayer obs, ItemParser items, String[] f) throws Exception {
        switch (f[0]) {
            case "reset" -> {
                p.closeContainer();
                for (int x = -3; x <= 12; x++) for (int z = -3; z <= 12; z++) for (int y = 99; y <= 106; y++)
                    level.setBlock(new BlockPos(x, y, z), y == 99 ? Blocks.STONE.defaultBlockState() : Blocks.AIR.defaultBlockState(), 2 | 16);
                drain(p);
                drain(obs);
                Ref.out.println("reset");
            }
            case "set" -> level.setBlock(pos(f, 1), BlockStateParser.parseForBlock(BuiltInRegistries.BLOCK, f[4], false).blockState(), 3);
            case "break" -> level.destroyBlock(pos(f, 1), true, null, 512);
            case "get" -> Ref.out.println("get " + f[1] + " " + f[2] + " " + f[3] + " " + BlockStateParser.serialize(level.getBlockState(pos(f, 1))));
            case "place" -> {
                BlockPos c = pos(f, 1);
                Direction face = Direction.byName(f[4]);
                Vec3 hit = new Vec3(c.getX() + Double.parseDouble(f[5]), c.getY() + Double.parseDouble(f[6]), c.getZ() + Double.parseDouble(f[7]));
                p.setYRot(Float.parseFloat(f[8]));
                p.setXRot(Float.parseFloat(f[9]));
                p.setShiftKeyDown(f[10].equals("1"));
                ItemStack st = new ItemStack(items.parse(new StringReader(f[11])).item(), 64);
                p.setItemInHand(InteractionHand.MAIN_HAND, st);
                var r = p.gameMode.useItemOn(p, level, st, InteractionHand.MAIN_HAND, new BlockHitResult(hit, face, c, false));
                p.setShiftKeyDown(false);
                Ref.out.println("placed " + r.getClass().getSimpleName());
            }
            case "use" -> {
                BlockPos c = pos(f, 1);
                p.setShiftKeyDown(f.length > 4 && f[4].equals("1"));
                p.setItemInHand(InteractionHand.MAIN_HAND, ItemStack.EMPTY);
                var r = p.gameMode.useItemOn(p, level, ItemStack.EMPTY, InteractionHand.MAIN_HAND, new BlockHitResult(Vec3.atCenterOf(c), Direction.UP, c, false));
                p.setShiftKeyDown(false);
                Ref.out.println("used " + r.getClass().getSimpleName() + " menu=" + p.containerMenu.slots.size());
            }
            case "close" -> p.closeContainer();
            case "slot0" -> {
                p.containerMenu.getSlot(0).set(new ItemStack(net.minecraft.world.item.Items.DIAMOND));
                for (int x = -3; x <= 12; x++) for (int z = -3; z <= 12; z++) {
                    BlockPos q = new BlockPos(x, 100, z);
                    if (level.getBlockEntity(q) instanceof net.minecraft.world.Container c && !c.getItem(0).isEmpty())
                        Ref.out.println("slot0 " + q.toShortString());
                }
            }
            case "drain" -> {
                for (Packet<?> pk : drain(p)) {
                    if (pk instanceof ClientboundOpenScreenPacket o)
                        Ref.out.println("screen " + BuiltInRegistries.MENU.getKey(o.getType()) + " " + o.getTitle().getContents());
                }
                for (Packet<?> pk : drain(obs)) {
                    if (pk instanceof ClientboundBlockEventPacket b)
                        Ref.out.println("blockevent " + b.getPos().toShortString() + " " + b.getB0() + " " + b.getB1() + " " + BuiltInRegistries.BLOCK.getKey(b.getBlock()));
                    else if (pk instanceof ClientboundSoundPacket s)
                        Ref.out.println("sound " + s.getSound().unwrapKey().map(k -> k.identifier().toString()).orElse("?") + " " + s.getSource() + " " + s.getX() + " " + s.getY() + " " + s.getZ() + " v=" + s.getVolume() + " p=" + s.getPitch());
                    else if (pk instanceof ClientboundBlockUpdatePacket u)
                        Ref.out.println("update " + u.getPos().toShortString() + " " + BlockStateParser.serialize(u.getBlockState()));
                    else if (pk instanceof ClientboundSectionBlocksUpdatePacket u)
                        u.runUpdates((bp, bs) -> Ref.out.println("update " + bp.toShortString() + " " + BlockStateParser.serialize(bs)));
                }
            }
            default -> Ref.out.println("error unknown op " + f[0]);
        }
    }

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
}
