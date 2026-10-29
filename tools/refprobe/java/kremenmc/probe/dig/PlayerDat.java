package kremenmc.probe.dig;

import java.nio.file.*;
import java.util.*;
import com.mojang.authlib.GameProfile;
import net.minecraft.nbt.NbtAccounter;
import net.minecraft.nbt.NbtIo;
import net.minecraft.server.level.ClientInformation;
import net.minecraft.server.level.ServerPlayer;
import net.minecraft.util.ProblemReporter;
import net.minecraft.world.level.storage.TagValueInput;

/** Loads a kremenmc-written playerdata file with the reference ServerPlayer.load and prints what it read. */
public final class PlayerDat {
    public static void main(String[] args) throws Exception {
        var server = Ref.boot();
        server.submit(() -> {
            net.minecraft.nbt.CompoundTag tag;
            try {
                tag = NbtIo.readCompressed(Path.of(args[0]), NbtAccounter.unlimitedHeap());
            } catch (java.io.IOException e) {
                throw new RuntimeException(e);
            }
            var p = new ServerPlayer(server, server.overworld(), new GameProfile(UUID.randomUUID(), "probe"), ClientInformation.createDefault());
            var problems = new ProblemReporter.Collector();
            p.load(TagValueInput.create(problems, server.registryAccess(), tag));
            Ref.out.println("pos " + p.getX() + " " + p.getY() + " " + p.getZ() + " rot " + p.getYRot() + " " + p.getXRot());
            Ref.out.println("health " + p.getHealth() + " food " + p.getFoodData().getFoodLevel() + " sat " + p.getFoodData().getSaturationLevel() + " xp " + p.experienceLevel + " " + p.experienceProgress + " slot " + p.getInventory().getSelectedSlot());
            for (int i = 0; i < p.getInventory().getContainerSize(); i++) {
                var s = p.getInventory().getItem(i);
                if (!s.isEmpty()) Ref.out.println("slot " + i + " " + s + " " + s.getComponentsPatch());
            }
            for (var e : p.getActiveEffects()) Ref.out.println("effect " + e);
            Ref.out.println("problems " + (problems.isEmpty() ? "none" : problems.getReport()));
            return null;
        }).get();
        Ref.exit();
    }
}
