package kremenmc.probe.item;

import java.nio.file.*;
import java.util.*;
import com.mojang.brigadier.StringReader;
import it.unimi.dsi.fastutil.ints.Int2ObjectOpenHashMap;
import net.minecraft.commands.arguments.item.ItemParser;
import net.minecraft.network.HashedPatchMap;
import net.minecraft.network.HashedStack;
import net.minecraft.network.protocol.Packet;
import net.minecraft.network.protocol.game.*;
import net.minecraft.util.HashOps;
import net.minecraft.world.inventory.ContainerInput;
import net.minecraft.world.item.ItemStack;
import net.minecraft.world.level.GameType;

/**
 * Container sync oracle: sets up a player inventory, syncs it, then feeds
 * one ServerboundContainerClickPacket (client prediction from the case) to
 * the real packet handler and prints every packet sent back.
 */
public final class Sync {
    public static void main(String[] args) throws Exception {
        var server = Ref.boot();
        var level = server.overworld();
        var ops = server.registryAccess().createSerializationContext(HashOps.CRC32C_INSTANCE);
        HashedPatchMap.HashGenerator gen = c -> c.encodeValue(ops).getOrThrow().asInt();
        ItemParser parser = new ItemParser(server.registryAccess());
        Map<Integer, ItemStack> palette = new HashMap<>();
        List<String> lines = Files.readAllLines(Path.of(args[0]));
        server.submit(() -> {
            for (String line : lines) {
                String[] f = line.strip().split(" ");
                try {
                    if (f[0].equals("palette")) {
                        palette.put(Integer.parseInt(f[1]), parser.parse(new StringReader(line.strip().split(" ", 3)[2])).createItemStack(1));
                        continue;
                    }
                    if (!f[0].equals("case")) continue;
                    FakePlayer p = new FakePlayer(level);
                    p.setGameMode(f[2].equals("c") ? GameType.CREATIVE : GameType.SURVIVAL);
                    var menu = p.inventoryMenu;
                    if (!f[3].equals("-")) {
                        for (String e : f[3].split(",")) {
                            String[] kv = e.split("=");
                            menu.getSlot(Integer.parseInt(kv[0])).set(stack(palette, kv[1]));
                        }
                    }
                    if (!f[4].equals("-")) menu.setCarried(stack(palette, f[4]));
                    p.initInventoryMenu();
                    String initPk = p.sent().size() + "";
                    menu.sendAllDataToRemote();
                    p.sent();
                    StringBuilder sb = new StringBuilder("sync ").append(f[1]).append(" state=").append(menu.getStateId()).append(" init=").append(initPk);
                    String[] c = f[5].split("/");
                    var changed = new Int2ObjectOpenHashMap<HashedStack>();
                    if (!f[7].equals("-")) {
                        for (String e : f[7].split(",")) {
                            String[] kv = e.split("=");
                            changed.put(Integer.parseInt(kv[0]), HashedStack.create(stack(palette, kv[1]), gen));
                        }
                    }
                    var pk = new ServerboundContainerClickPacket(0, menu.getStateId() + Integer.parseInt(f[6]), Short.parseShort(c[0]),
                            Byte.parseByte(c[1]), ContainerInput.values()[Integer.parseInt(c[2])], changed, HashedStack.create(stack(palette, f[8]), gen));
                    p.connection.handleContainerClick(pk);
                    for (Packet<?> out : p.sent()) {
                        sb.append(' ');
                        if (out instanceof ClientboundContainerSetSlotPacket s) {
                            sb.append("slot:").append(s.getContainerId()).append(':').append(s.getStateId()).append(':').append(s.getSlot()).append(':').append(Clicks.hex(s.getItem()));
                        } else if (out instanceof ClientboundContainerSetContentPacket s) {
                            sb.append("content:").append(s.containerId()).append(':').append(s.stateId()).append(':');
                            for (int i = 0; i < s.items().size(); i++) sb.append(i == 0 ? "" : ".").append(Clicks.hex(s.items().get(i)));
                            sb.append(':').append(Clicks.hex(s.carriedItem()));
                        } else if (out instanceof ClientboundSetCursorItemPacket s) {
                            sb.append("cursor:").append(Clicks.hex(s.contents()));
                        } else if (out instanceof ClientboundSetPlayerInventoryPacket s) {
                            sb.append("inv:").append(s.slot()).append(':').append(Clicks.hex(s.contents()));
                        } else {
                            sb.append(out.getClass().getSimpleName());
                        }
                    }
                    sb.append(" end=").append(menu.getStateId());
                    Ref.out.println(sb);
                } catch (Exception e) {
                    Ref.out.println("error " + f[1] + " " + e);
                }
            }
            return null;
        }).get();
        Ref.exit();
    }

    static ItemStack stack(Map<Integer, ItemStack> palette, String spec) {
        if (spec.equals("e")) return ItemStack.EMPTY;
        String[] pc = spec.split(":");
        return palette.get(Integer.parseInt(pc[0])).copyWithCount(Integer.parseInt(pc[1]));
    }
}
