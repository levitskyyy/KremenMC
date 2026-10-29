package kremenmc.probe.biome;

import com.mojang.datafixers.util.Pair;
import java.io.BufferedReader;
import java.io.InputStreamReader;
import java.lang.reflect.Field;
import java.nio.file.Files;
import java.nio.file.Path;
import java.util.ArrayList;
import java.util.List;
import net.minecraft.SharedConstants;
import net.minecraft.world.level.biome.Climate;

// ClimateLab <script|-> runs a small command script against the reference
// ParameterList (black box; reflection only reads the built tree's fields):
//
//	E t0 t1 h0 h1 c0 c1 e0 e1 d0 d1 w0 w1 offset name   add entry (quantized)
//	BUILD [k]      build list from entries (k = rebuildWithChildrenPerNode)
//	DUMP           print the tree (node spaces, leaf values)
//	Q t h c e d w  findValue on the current thread
//	QB t h c e d w findValue and findValueBruteForce
//	QT t h c e d w findValue on a fresh thread (empty per-thread state)
//	CLEAR          reset the per-thread lastResult
//	RESET          drop entries
//	REPORT path    add entries from a biome_parameters report
//	PRINTE         print entries as E lines
public final class ClimateLab {
    static Climate.ParameterList<String> pl;
    static Object tree;

    public static void main(String[] args) throws Exception {
        SharedConstants.tryDetectVersion();
        BufferedReader in = args[0].equals("-") ? new BufferedReader(new InputStreamReader(System.in))
                : Files.newBufferedReader(Path.of(args[0]));
        List<Pair<Climate.ParameterPoint, String>> entries = new ArrayList<>();
        StringBuilder out = new StringBuilder();
        String line;
        while ((line = in.readLine()) != null) {
            String[] f = line.trim().split("\\s+");
            if (f.length == 0 || f[0].isEmpty() || f[0].startsWith("#")) continue;
            switch (f[0]) {
                case "E" -> {
                    long[] v = new long[13];
                    for (int i = 0; i < 13; i++) v[i] = Long.parseLong(f[1 + i]);
                    Climate.ParameterPoint pp = new Climate.ParameterPoint(new Climate.Parameter(v[0], v[1]),
                            new Climate.Parameter(v[2], v[3]), new Climate.Parameter(v[4], v[5]),
                            new Climate.Parameter(v[6], v[7]), new Climate.Parameter(v[8], v[9]),
                            new Climate.Parameter(v[10], v[11]), v[12]);
                    entries.add(Pair.of(pp, f[14]));
                }
                case "RESET" -> entries.clear();
                case "REPORT" -> entries.addAll(ClimateProbe.load(Path.of(f[1])));
                case "PRINTE" -> {
                    for (Pair<Climate.ParameterPoint, String> e : entries) {
                        Climate.ParameterPoint p = e.getFirst();
                        out.append('E');
                        for (Climate.Parameter q : List.of(p.temperature(), p.humidity(), p.continentalness(), p.erosion(),
                                p.depth(), p.weirdness())) out.append(' ').append(q.min()).append(' ').append(q.max());
                        out.append(' ').append(p.offset()).append(' ').append(e.getSecond()).append('\n');
                    }
                }
                case "BUILD" -> {
                    pl = new Climate.ParameterList<>(new ArrayList<>(entries));
                    if (f.length > 1) pl = pl.rebuildWithChildrenPerNode(Integer.parseInt(f[1]));
                    tree = get(pl, "index");
                }
                case "DUMP" -> dump(get(tree, "root"), 0, out);
                case "Q" -> out.append(pl.findValue(tp(f))).append('\n');
                case "QB" -> {
                    Climate.TargetPoint t = tp(f);
                    out.append(pl.findValue(t)).append(' ').append(pl.findValueBruteForce(t)).append('\n');
                }
                case "QT" -> {
                    Climate.TargetPoint t = tp(f);
                    String[] r = new String[1];
                    Thread th = new Thread(() -> r[0] = pl.findValue(t));
                    th.start();
                    th.join();
                    out.append(r[0]).append('\n');
                }
                case "CLEAR" -> ((ThreadLocal<?>) get(tree, "lastResult")).remove();
                default -> throw new IllegalArgumentException("bad command: " + line);
            }
            if (out.length() > 1 << 16) {
                System.out.print(out);
                out.setLength(0);
            }
        }
        System.out.print(out);
        System.out.flush();
    }

    static Climate.TargetPoint tp(String[] f) {
        return new Climate.TargetPoint(Long.parseLong(f[1]), Long.parseLong(f[2]), Long.parseLong(f[3]),
                Long.parseLong(f[4]), Long.parseLong(f[5]), Long.parseLong(f[6]));
    }

    static Object get(Object o, String name) throws Exception {
        for (Class<?> c = o.getClass(); c != null; c = c.getSuperclass()) {
            try {
                Field fl = c.getDeclaredField(name);
                fl.setAccessible(true);
                return fl.get(o);
            } catch (NoSuchFieldException ignored) {
            }
        }
        throw new NoSuchFieldException(name);
    }

    static void dump(Object node, int depth, StringBuilder out) throws Exception {
        out.append("  ".repeat(depth));
        Climate.Parameter[] space = (Climate.Parameter[]) get(node, "parameterSpace");
        boolean leaf = node.getClass().getSimpleName().equals("Leaf");
        out.append(leaf ? "L" : "N");
        for (Climate.Parameter p : space) out.append(' ').append(p.min()).append(':').append(p.max());
        if (leaf) {
            out.append(' ').append(get(node, "value")).append('\n');
            return;
        }
        Object[] ch = (Object[]) get(node, "children");
        out.append(" n=").append(ch.length).append('\n');
        for (Object c : ch) dump(c, depth + 1, out);
    }
}
