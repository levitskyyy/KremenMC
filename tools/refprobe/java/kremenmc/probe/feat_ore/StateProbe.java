package kremenmc.probe.feat_ore;

import com.google.gson.JsonParser;
import com.mojang.serialization.JsonOps;
import java.io.PrintStream;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import net.minecraft.SharedConstants;
import net.minecraft.core.BlockPos;
import net.minecraft.core.registries.Registries;
import net.minecraft.data.registries.VanillaRegistries;
import net.minecraft.resources.Identifier;
import net.minecraft.resources.RegistryOps;
import net.minecraft.resources.ResourceKey;
import net.minecraft.util.valueproviders.FloatProviders;
import net.minecraft.util.valueproviders.IntProviders;
import net.minecraft.world.level.levelgen.WorldgenRandom;
import net.minecraft.world.level.levelgen.XoroshiroRandomSource;
import net.minecraft.world.level.levelgen.placement.PlacementModifier;

// StateProbe is Mods with the full block state printed. Mods samples value providers, context-free placement modifiers and block
// state providers with a seeded xoroshiro WorldgenRandom. Input lines:
//   int|float|mod <seed> <x> <y> <z> <json>
//   bsp <seed> <x> <y> <z> <block_state_provider registry key>
// Output: the input line, " => ", the results, " | " and the next long of
// the random (so the number of draws is checked too).
public final class StateProbe {
    public static void main(String[] args) throws Exception {
        PrintStream out = System.out;
        System.setOut(System.err);
        SharedConstants.tryDetectVersion();
        net.minecraft.server.Bootstrap.bootStrap();
        var lookup = VanillaRegistries.createWorldLookup();
        var ops = RegistryOps.create(JsonOps.INSTANCE, lookup);
        var bsps = lookup.lookupOrThrow(Registries.BLOCK_STATE_PROVIDER);
        for (String line : Files.readAllLines(Path.of(args[0]))) {
            if (line.isBlank()) continue;
            String[] f = line.split(" ", 6);
            long seed = Long.parseLong(f[1]);
            BlockPos pos = new BlockPos(Integer.parseInt(f[2]), Integer.parseInt(f[3]), Integer.parseInt(f[4]));
            var r = new WorldgenRandom(new XoroshiroRandomSource(seed));
            List<String> res = new ArrayList<>();
            switch (f[0]) {
                case "int" -> {
                    var p = IntProviders.CODEC.parse(ops, JsonParser.parseString(f[5])).getOrThrow();
                    for (int i = 0; i < 8; i++) res.add(Integer.toString(p.sample(r)));
                }
                case "float" -> {
                    var p = FloatProviders.CODEC.parse(ops, JsonParser.parseString(f[5])).getOrThrow();
                    for (int i = 0; i < 8; i++) res.add(Float.toString(p.sample(r)));
                }
                case "mod" -> {
                    var m = PlacementModifier.CODEC.parse(ops, JsonParser.parseString(f[5])).getOrThrow();
                    m.modify(null, r, pos, p -> res.add(p.getX() + "," + p.getY() + "," + p.getZ()));
                }
                case "bsp" -> {
                    var p = bsps.getOrThrow(ResourceKey.create(Registries.BLOCK_STATE_PROVIDER, Identifier.parse(f[5]))).value();
                    res.add(String.valueOf(p.getState(null, r, pos)));
                }
                default -> throw new IllegalArgumentException(line);
            }
            out.println(line + " => " + String.join(" ", res) + " | " + r.nextLong());
        }
        out.flush();
        Runtime.getRuntime().halt(0);
    }
}
