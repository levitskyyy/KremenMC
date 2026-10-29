package kremenmc.probe.biome;

import com.google.gson.JsonArray;
import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import com.mojang.datafixers.util.Pair;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import java.util.SplittableRandom;
import net.minecraft.SharedConstants;
import net.minecraft.world.level.biome.Climate;

// ClimateProbe <parameters.json> <mode> <count> <seed>
// Prints one line per target point: t h c e d w findValue bruteForce.
// mode: random | boundary | sequence (sequence also prints findValue after a
// different preceding query, to expose any search cache effects).
public final class ClimateProbe {
    static Climate.Parameter param(JsonElement e) {
        if (e.isJsonArray()) {
            JsonArray a = e.getAsJsonArray();
            return Climate.Parameter.span(a.get(0).getAsFloat(), a.get(1).getAsFloat());
        }
        return Climate.Parameter.point(e.getAsFloat());
    }

    static List<Pair<Climate.ParameterPoint, String>> load(Path path) throws Exception {
        JsonObject root = JsonParser.parseString(Files.readString(path)).getAsJsonObject();
        List<Pair<Climate.ParameterPoint, String>> list = new ArrayList<>();
        for (JsonElement el : root.getAsJsonArray("biomes")) {
            JsonObject o = el.getAsJsonObject();
            JsonObject p = o.getAsJsonObject("parameters");
            Climate.ParameterPoint pp = Climate.parameters(param(p.get("temperature")), param(p.get("humidity")),
                    param(p.get("continentalness")), param(p.get("erosion")), param(p.get("depth")),
                    param(p.get("weirdness")), p.get("offset").getAsFloat());
            list.add(Pair.of(pp, o.get("biome").getAsString()));
        }
        return list;
    }

    public static void main(String[] args) throws Exception {
        SharedConstants.tryDetectVersion();
        List<Pair<Climate.ParameterPoint, String>> list = load(Path.of(args[0]));
        List<long[]> bounds = new ArrayList<>();
        for (Pair<Climate.ParameterPoint, String> e : list) {
            Climate.ParameterPoint pp = e.getFirst();
            bounds.add(new long[] {pp.temperature().min(), pp.temperature().max(), pp.humidity().min(), pp.humidity().max(),
                    pp.continentalness().min(), pp.continentalness().max(), pp.erosion().min(), pp.erosion().max(),
                    pp.depth().min(), pp.depth().max(), pp.weirdness().min(), pp.weirdness().max()});
        }
        String mode = args[1];
        int n = Integer.parseInt(args[2]);
        SplittableRandom rnd = new SplittableRandom(Long.parseLong(args[3]));
        Climate.ParameterList<String> pl = new Climate.ParameterList<>(list);
        StringBuilder sb = new StringBuilder();
        long[] prev = null;
        for (int i = 0; i < n; i++) {
            long[] v = new long[6];
            for (int k = 0; k < 6; k++) {
                if (mode.equals("boundary")) {
                    long[] b = bounds.get(rnd.nextInt(bounds.size()));
                    v[k] = b[2 * k + rnd.nextInt(2)] + rnd.nextInt(-1, 2);
                } else {
                    v[k] = rnd.nextLong(-22000, 22000);
                }
            }
            Climate.TargetPoint tp = new Climate.TargetPoint(v[0], v[1], v[2], v[3], v[4], v[5]);
            if (mode.equals("sequence") && prev != null) {
                pl.findValue(new Climate.TargetPoint(prev[0], prev[1], prev[2], prev[3], prev[4], prev[5]));
            }
            String fv = pl.findValue(tp);
            String bf = pl.findValueBruteForce(tp);
            for (long x : v) sb.append(x).append(' ');
            sb.append(fv).append(' ').append(bf).append('\n');
            prev = v;
            if (sb.length() > 1 << 16) {
                System.out.print(sb);
                sb.setLength(0);
            }
        }
        System.out.print(sb);
    }
}
