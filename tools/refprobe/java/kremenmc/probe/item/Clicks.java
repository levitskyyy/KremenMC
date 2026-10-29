package kremenmc.probe.item;

import java.nio.file.*;
import java.util.*;
import com.mojang.authlib.GameProfile;
import com.mojang.brigadier.StringReader;
import net.minecraft.commands.arguments.item.ItemParser;
import net.minecraft.network.RegistryFriendlyByteBuf;
import net.minecraft.server.level.ClientInformation;
import net.minecraft.server.level.ServerLevel;
import net.minecraft.server.level.ServerPlayer;
import net.minecraft.util.Prediction;
import net.minecraft.world.entity.item.ItemEntity;
import net.minecraft.world.inventory.ContainerInput;
import net.minecraft.world.item.ItemStack;
import net.minecraft.world.level.GameType;

/**
 * Container click oracle: for each case in args[0], fills a fresh player's
 * inventory menu, applies the clicks and prints every slot, the carried
 * stack and the dropped stacks (OPTIONAL_STREAM_CODEC hex).
 */
public final class Clicks {
    static String hex(ItemStack s) {
        RegistryFriendlyByteBuf b = Ref.buf();
        ItemStack.OPTIONAL_STREAM_CODEC.encode(b, s);
        return Ref.hex(b);
    }

    public static void main(String[] args) throws Exception {
        var server = Ref.boot();
        var level = server.overworld();
        ItemParser parser = new ItemParser(server.registryAccess());
        Map<Integer, ItemStack> palette = new HashMap<>();
        List<String> lines = Files.readAllLines(Path.of(args[0]));
        server.submit(() -> {
            for (String line : lines) {
                String[] f = line.strip().split(" ");
                try {
                    if (f[0].equals("palette")) {
                        String spec = line.strip().split(" ", 3)[2];
                        ItemStack ps = parser.parse(new StringReader(spec)).createItemStack(1);
                        palette.put(Integer.parseInt(f[1]), ps);
                        Ref.out.println("palhex " + f[1] + " " + hex(ps));
                        continue;
                    }
                    if (!f[0].equals("case")) continue;
                    FakePlayer p = new FakePlayer(level);
                    p.setGameMode(f[2].equals("c") ? GameType.CREATIVE : GameType.SURVIVAL);
                    if (p.hasInfiniteMaterials() != f[2].equals("c")) throw new IllegalStateException("game mode not applied");
                    p.getInventory().setSelectedSlot(Integer.parseInt(f[3]));
                    var menu = p.inventoryMenu;
                    if (!f[5].equals("-")) {
                        for (String e : f[5].split(",")) {
                            String[] kv = e.split("=");
                            menu.getSlot(Integer.parseInt(kv[0])).set(stack(palette, kv[1]));
                        }
                    }
                    if (!f[4].equals("-")) menu.setCarried(stack(palette, f[4]));
                    p.drops.clear();
                    if (!f[6].equals("-")) {
                        for (String c : f[6].split(",")) {
                            String[] x = c.split("/");
                            menu.clicked(Integer.parseInt(x[0]), Integer.parseInt(x[1]), ContainerInput.values()[Integer.parseInt(x[2])], p);
                        }
                    }
                    StringBuilder sb = new StringBuilder("result ").append(f[1]).append(' ');
                    for (int i = 0; i < menu.slots.size(); i++) sb.append(i == 0 ? "" : ",").append(hex(menu.getSlot(i).getItem()));
                    sb.append(' ').append(hex(menu.getCarried())).append(' ');
                    sb.append(p.drops.isEmpty() ? "-" : String.join(",", p.drops));
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
        String[] pc = spec.split(":");
        return palette.get(Integer.parseInt(pc[0])).copyWithCount(Integer.parseInt(pc[1]));
    }
}
