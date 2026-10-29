package kremenmc.probe.item;

import java.nio.file.*;
import java.util.*;
import com.mojang.brigadier.StringReader;
import net.minecraft.commands.arguments.item.ItemParser;
import net.minecraft.core.registries.BuiltInRegistries;
import net.minecraft.network.protocol.Packet;
import net.minecraft.network.protocol.game.*;
import net.minecraft.world.InteractionHand;
import net.minecraft.world.entity.EquipmentSlot;
import net.minecraft.world.item.ItemStack;
import net.minecraft.world.level.GameType;

/**
 * Item use oracle: puts stacks in the hands and armor slots, sends one
 * ServerboundUseItemPacket to the real handler, runs up to n use ticks and
 * prints the resulting equipment, food, effects and sync packets.
 */
public final class Use {
    public static void main(String[] args) throws Exception {
        var server = Ref.boot();
        var level = server.overworld();
        ItemParser parser = new ItemParser(server.registryAccess());
        List<String> lines = Files.readAllLines(Path.of(args[0]));
        server.submit(() -> {
            for (String line : lines) {
                String[] f = line.strip().split("\\|");
                if (f.length < 2) continue;
                try {
                    FakePlayer p = new FakePlayer(level);
                    p.setGameMode(f[1].equals("c") ? GameType.CREATIVE : GameType.SURVIVAL);
                    p.getInventory().setSelectedSlot(0);
                    p.setItemSlot(EquipmentSlot.MAINHAND, stack(parser, f[2]));
                    p.setItemSlot(EquipmentSlot.OFFHAND, stack(parser, f[3]));
                    p.setItemSlot(EquipmentSlot.HEAD, stack(parser, f[4]));
                    p.setItemSlot(EquipmentSlot.CHEST, stack(parser, f[5]));
                    p.getFoodData().setFoodLevel(Integer.parseInt(f[6]));
                    p.getFoodData().setSaturation(Float.parseFloat(f[7]));
                    p.setHealth(Float.parseFloat(f[8]));
                    p.initInventoryMenu();
                    p.sent();
                    InteractionHand hand = f[9].equals("1") ? InteractionHand.OFF_HAND : InteractionHand.MAIN_HAND;
                    p.connection.handleUseItem(new ServerboundUseItemPacket(hand, 1, p.getYRot(), p.getXRot()));
                    List<String> pk = summarize(p.sent());
                    boolean using = p.isUsingItem();
                    int remaining = p.getUseItemRemainingTicks();
                    p.tickUse(Integer.parseInt(f[10]));
                    pk.addAll(summarize(p.sent()));
                    StringBuilder sb = new StringBuilder("use ").append(f[0]);
                    sb.append(" using=").append(using).append(" remaining=").append(remaining).append(" after=").append(p.isUsingItem());
                    sb.append(" main=").append(Clicks.hex(p.getMainHandItem())).append(" off=").append(Clicks.hex(p.getOffhandItem()));
                    sb.append(" head=").append(Clicks.hex(p.getItemBySlot(EquipmentSlot.HEAD))).append(" chest=").append(Clicks.hex(p.getItemBySlot(EquipmentSlot.CHEST)));
                    sb.append(" food=").append(p.getFoodData().getFoodLevel()).append('/').append(p.getFoodData().getSaturationLevel());
                    sb.append(" health=").append(p.getHealth());
                    List<String> effects = new ArrayList<>();
                    for (var e : p.getActiveEffects()) effects.add(e.getEffect().unwrapKey().get().identifier() + ":" + e.getAmplifier() + ":" + e.getDuration());
                    Collections.sort(effects);
                    sb.append(" effects=").append(effects.isEmpty() ? "-" : String.join(",", effects));
                    List<String> inv = new ArrayList<>();
                    for (int i = 0; i < 36; i++) {
                        if (i != 0 && !p.getInventory().getItem(i).isEmpty()) inv.add(i + ":" + Clicks.hex(p.getInventory().getItem(i)));
                    }
                    sb.append(" inv=").append(inv.isEmpty() ? "-" : String.join(",", inv));
                    sb.append(" drops=").append(p.drops.isEmpty() ? "-" : String.join(",", p.drops));
                    sb.append(" packets=").append(pk.isEmpty() ? "-" : String.join(",", pk));
                    Ref.out.println(sb);
                } catch (Throwable e) {
                    Ref.out.println("error " + f[0] + " " + e);
                }
            }
            return null;
        }).get();
        Ref.exit();
    }

    static List<String> summarize(List<Packet<?>> ps) {
        List<String> out = new ArrayList<>();
        for (Packet<?> o : ps) {
            if (o instanceof ClientboundContainerSetContentPacket) out.add("content");
            else if (o instanceof ClientboundContainerSetSlotPacket s) out.add("slot" + s.getSlot());
            else if (o instanceof ClientboundCooldownPacket c) out.add("cooldown:" + c.cooldownGroup() + ":" + c.duration());
        }
        return out;
    }

    static final Set<String> printed = new HashSet<>();

    static ItemStack stack(ItemParser parser, String spec) throws Exception {
        if (spec.equals("-")) return ItemStack.EMPTY;
        String[] f = spec.split(" ", 2);
        var in = parser.parse(new StringReader(f[1]));
        ItemStack s = new ItemStack(in.item(), Integer.parseInt(f[0]), in.components());
        if (printed.add(spec)) Ref.out.println("stackspec " + Clicks.hex(s) + "\t" + spec);
        return s;
    }
}
