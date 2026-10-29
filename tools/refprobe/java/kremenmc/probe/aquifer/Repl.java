package kremenmc.probe.aquifer;

import java.io.BufferedReader;
import java.io.InputStreamReader;
import java.lang.reflect.Array;
import java.lang.reflect.Field;
import java.lang.reflect.Method;
import java.util.ArrayList;
import java.util.List;
import net.minecraft.world.level.block.state.BlockState;
import net.minecraft.world.level.levelgen.Aquifer;
import org.apache.commons.lang3.mutable.MutableDouble;

// Line-oriented black-box driver for Aquifer$NoiseBasedAquifer.
//
//	new <settings> <seed> <cx> <cz>   build a NoiseChunk and take its aquifer
//	cfg <settings> <seed> <cx> <cz> <picker> <barrier> <exclusion> <floodedness> <spread> <lava> <surface>
//	                                  aquifer from a custom config (functions as JSON without spaces);
//	                                  picker: std | <lavaBelowY>:<lavaLevel>:<seaLevel>:<fluid> (calls logged)
//	                                  a function "name!<json>" logs its sample points
//	                                  "T:<table>:<c|xz|p>:<default>" reads a table set by tset
//	setcell <gx> <gy> <gz> <x> <y> <z> <fs>  preset a grid cell's center and status
//	tset <table> <key>=<v>...         key: cx,cz | x,z | x,y,z
//	tclear                            drop all tables
//	fnlog                             logged function samples since the last fnlog
//	picklog                           fluid picker calls since the last picklog
//	call <method> <arg>...            arg: int, d:<double>, fs:<level>:<block>, md:<double>, j:<long>
//	get <field>                       print a field of the aquifer
//	sample <fn> <x> <y> <z>           config function: uncached and through the aquifer's bound sampler
//	sub <x> <y> <z> <density>         computeSubstance + shouldScheduleFluidUpdate
//	rand <x> <y> <z>                  positionalRandomFactory.at(x,y,z): first 4 nextInt(10) and nextLong
public final class Repl {
    static Aquifer aq;
    static Env.Chunk ch;

    public static void main(String[] args) throws Exception {
        Env.init();
        BufferedReader in = new BufferedReader(new InputStreamReader(System.in), 1 << 16);
        String line;
        while ((line = in.readLine()) != null) {
            line = line.trim();
            if (line.isEmpty()) continue;
            try {
                Env.out.println(run(line.split("\\s+")));
            } catch (Throwable t) {
                Throwable c = t.getCause() != null ? t.getCause() : t;
                Env.out.println("err " + c);
            }
            if (!in.ready()) Env.out.flush();
        }
        Env.out.flush();
    }

    static String run(String[] f) throws Exception {
        switch (f[0]) {
            case "new" -> {
                ch = Env.chunk(f[1], Long.parseLong(f[2]), Integer.parseInt(f[3]), Integer.parseInt(f[4]));
                aq = ch.aquifer();
                return "ok " + aq.getClass().getName();
            }
            case "cfg" -> {
                ch = Env.custom(f[1], Long.parseLong(f[2]), Integer.parseInt(f[3]), Integer.parseInt(f[4]), f[5], java.util.Arrays.copyOfRange(f, 6, 12));
                aq = ch.aquifer();
                return "ok " + aq.getClass().getName();
            }
            case "setcell" -> {
                int idx = (Integer) method("getIndex", 3).invoke(aq, Integer.parseInt(f[1]), Integer.parseInt(f[2]), Integer.parseInt(f[3]));
                long[] locs = (long[]) field("aquiferLocationCache").get(aq);
                Aquifer.FluidStatus[] sts = (Aquifer.FluidStatus[]) field("aquiferCache").get(aq);
                locs[idx] = net.minecraft.core.BlockPos.asLong(Integer.parseInt(f[4]), Integer.parseInt(f[5]), Integer.parseInt(f[6]));
                sts[idx] = (Aquifer.FluidStatus) arg(f[7], Aquifer.FluidStatus.class);
                return "ok " + idx;
            }
            case "tset" -> {
                var t = TableFn.tables.computeIfAbsent(f[1], k -> new java.util.HashMap<>());
                for (int i = 2; i < f.length; i++) {
                    String[] kv = f[i].split("=");
                    t.put(kv[0], Float.parseFloat(kv[1]));
                }
                return "ok";
            }
            case "tclear" -> {
                TableFn.tables.clear();
                return "ok";
            }
            case "fnlog" -> {
                String r = String.join(" ", LogFn.log);
                LogFn.log.clear();
                return "[" + r + "]";
            }
            case "picklog" -> {
                String r = String.join(" ", Env.pickLog);
                Env.pickLog.clear();
                return "[" + r + "]";
            }
            case "get" -> {
                Field fl = field(f[1]);
                return fmt(fl.get(aq));
            }
            case "call" -> {
                Method m = method(f[1], f.length - 2);
                Object[] a = new Object[f.length - 2];
                List<MutableDouble> mds = new ArrayList<>();
                Class<?>[] pt = m.getParameterTypes();
                for (int i = 0; i < a.length; i++) {
                    a[i] = arg(f[2 + i], pt[i]);
                    if (a[i] instanceof MutableDouble md) mds.add(md);
                }
                Object r = m.invoke(aq, a);
                StringBuilder sb = new StringBuilder(fmt(r));
                for (MutableDouble md : mds) sb.append(" md=").append(Env.d(md.doubleValue()));
                return sb.toString();
            }
            case "sub" -> {
                BlockState s = aq.computeSubstance(Integer.parseInt(f[1]), Integer.parseInt(f[2]), Integer.parseInt(f[3]), Double.parseDouble(f[4]));
                return fmt(s) + " " + aq.shouldScheduleFluidUpdate();
            }
            case "sample" -> {
                int x = Integer.parseInt(f[2]), y = Integer.parseInt(f[3]), z = Integer.parseInt(f[4]);
                return Env.f(ch.uncached(f[1], x, y, z)) + " " + Env.f(ch.bound(aq, f[1], x, y, z));
            }
            case "rand" -> {
                var r = ch.positional(aq).at(Integer.parseInt(f[1]), Integer.parseInt(f[2]), Integer.parseInt(f[3]));
                return r.nextInt(10) + " " + r.nextInt(9) + " " + r.nextInt(10) + " " + r.nextLong();
            }
            default -> throw new IllegalArgumentException("unknown command " + f[0]);
        }
    }

    static Field field(String name) throws Exception {
        for (Class<?> c = aq.getClass(); c != null; c = c.getSuperclass()) {
            try {
                Field fl = c.getDeclaredField(name);
                fl.setAccessible(true);
                return fl;
            } catch (NoSuchFieldException e) {
                // try superclass
            }
        }
        throw new NoSuchFieldException(name);
    }

    static Method method(String name, int n) throws Exception {
        for (Method m : aq.getClass().getDeclaredMethods()) {
            if (m.getName().equals(name) && m.getParameterCount() == n) {
                m.setAccessible(true);
                return m;
            }
        }
        throw new NoSuchMethodException(name + "/" + n);
    }

    static Object arg(String s, Class<?> t) {
        if (s.startsWith("fs:")) {
            String[] p = s.split(":", 3);
            return new Aquifer.FluidStatus(Integer.parseInt(p[1]), Env.state(p[2]));
        }
        if (s.startsWith("md:")) return new MutableDouble(Double.parseDouble(s.substring(3)));
        if (s.startsWith("d:")) return Double.parseDouble(s.substring(2));
        if (s.startsWith("j:")) return Long.parseLong(s.substring(2));
        if (t == long.class) return Long.parseLong(s);
        if (t == double.class) return Double.parseDouble(s);
        if (t == boolean.class) return Boolean.parseBoolean(s);
        return Integer.parseInt(s);
    }

    static String fmt(Object o) {
        if (o == null) return "null";
        if (o instanceof BlockState s) return Env.name(s);
        if (o instanceof Aquifer.FluidStatus fs) return "fs:" + fs.fluidLevel() + ":" + Env.name(fs.fluidType());
        if (o instanceof Double d) return Env.d(d);
        if (o instanceof Float v) return Env.f(v);
        if (o.getClass().isArray()) {
            StringBuilder sb = new StringBuilder("[");
            int n = Array.getLength(o);
            for (int i = 0; i < n; i++) {
                if (i > 0) sb.append(' ');
                sb.append(fmt(Array.get(o, i)));
            }
            return sb.append(']').toString();
        }
        return o.toString();
    }
}
