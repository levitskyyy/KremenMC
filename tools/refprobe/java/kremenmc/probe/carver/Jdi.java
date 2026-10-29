package kremenmc.probe.carver;

import com.sun.jdi.*;
import com.sun.jdi.connect.*;
import com.sun.jdi.event.*;
import com.sun.jdi.request.*;
import java.io.*;
import java.util.*;

// Jdi runs another probe main class in a debuggee JVM and prints, in
// order, its Hook.ev events and the argument values of every entry into
// the named reference methods (observation only; nothing is stepped).
// Args: <Class#method,...> <main class> [args...]
public final class Jdi {
    public static void main(String[] a) throws Exception {
        Map<String, Set<String>> watch = new HashMap<>();
        Set<String> exits = new HashSet<>();
        Map<String, String> armed = new HashMap<>();
        for (String s : a[0].split(",")) {
            if (s.startsWith("+")) {
                String[] p = s.substring(1).split("@");
                armed.put(p[0], p[1]);
                continue;
            }
            if (s.startsWith("!")) {
                exits.add(s.substring(1));
                continue;
            }
            String[] p = s.split("#");
            watch.computeIfAbsent(p[0], k -> new HashSet<>()).add(p[1]);
        }
        watch.computeIfAbsent("kremenmc.probe.carver.Hook", k -> new HashSet<>()).add("ev");
        LaunchingConnector lc = Bootstrap.virtualMachineManager().defaultConnector();
        Map<String, Connector.Argument> args = lc.defaultArguments();
        args.get("main").setValue(String.join(" ", Arrays.copyOfRange(a, 1, a.length)));
        String extra = System.getenv("KREMENMC_JDI_OPTS") == null ? "" : System.getenv("KREMENMC_JDI_OPTS") + " ";
        args.get("options").setValue(extra + "-Xmx4g -Dkremenmc.jdi=1 -cp " + System.getProperty("java.class.path"));
        VirtualMachine vm = lc.launch(args);
        Process proc = vm.process();
        Thread err = pump(proc.getErrorStream(), System.err);
        String outFile = System.getenv("KREMENMC_JDI_OUT");
        Thread out = pump(proc.getInputStream(), outFile != null ? System.out : System.err);
        PrintStream o = new PrintStream(new BufferedOutputStream(outFile != null ? new FileOutputStream(outFile) : new FileOutputStream(FileDescriptor.out), 1 << 16), false);
        EventRequestManager erm = vm.eventRequestManager();
        for (String cls : watch.keySet()) {
            ClassPrepareRequest cp = erm.createClassPrepareRequest();
            cp.addClassFilter(cls);
            cp.setSuspendPolicy(EventRequest.SUSPEND_ALL);
            cp.enable();
        }
        List<MethodExitRequest> mxs = new ArrayList<>();
        for (String cls : exits) {
            MethodExitRequest mx = erm.createMethodExitRequest();
            mx.addClassFilter(cls);
            mx.setSuspendPolicy(EventRequest.SUSPEND_EVENT_THREAD);
            mxs.add(mx);
        }
        Map<String, List<MethodExitRequest>> triggers = new HashMap<>();
        for (var e : armed.entrySet()) {
            MethodExitRequest mx = erm.createMethodExitRequest();
            mx.addClassFilter(e.getKey());
            mx.setSuspendPolicy(EventRequest.SUSPEND_EVENT_THREAD);
            triggers.computeIfAbsent(e.getValue(), k -> new ArrayList<>()).add(mx);
        }
        EventQueue q = vm.eventQueue();
        List<EventRequest> toggled = new ArrayList<>();
        boolean[] on = {false};
        boolean done = false;
        while (!done) {
            EventSet es = q.remove();
            for (Event e : es) {
                if (e instanceof ClassPrepareEvent cpe) {
                    ReferenceType t = cpe.referenceType();
                    for (Method m : t.methods()) {
                        if (watch.getOrDefault(t.name(), Set.of()).contains(m.name()) && m.location() != null) {
                            BreakpointRequest br = erm.createBreakpointRequest(m.location());
                            br.setSuspendPolicy(EventRequest.SUSPEND_EVENT_THREAD);
                            if (t.name().endsWith(".Hook")) br.enable();
                            else {
                                toggled.add(br);
                                br.setEnabled(on[0]);
                            }
                        }
                    }
                } else if (e instanceof BreakpointEvent be) {
                    StackFrame f = be.thread().frame(0);
                    Method m = be.location().method();
                    List<Value> vs = f.getArgumentValues();
                    if (m.name().equals("ev") && m.declaringType().name().endsWith("Hook")) {
                        String ev = ((StringReference) vs.get(0)).value();
                        if (ev.startsWith("chunk") || ev.equals("END")) triggers.values().forEach(l -> l.forEach(EventRequest::disable));
                        if (ev.equals("BEGIN")) {
                            on[0] = true;
                            mxs.forEach(EventRequest::enable);
                            toggled.forEach(EventRequest::enable);
                        } else if (ev.equals("END")) {
                            on[0] = false;
                            mxs.forEach(EventRequest::disable);
                            toggled.forEach(EventRequest::disable);
                        }
                        else o.println(ev);
                    } else {
                        for (MethodExitRequest mx : triggers.getOrDefault(m.name(), List.of())) mx.enable();
                        StringBuilder sb = new StringBuilder(THREADS ? "E [" + be.thread().name() + "] " : "E ").append(m.declaringType().name().replaceAll(".*\\.", "")).append('.').append(m.name());
                        for (Value v : vs) sb.append(' ').append(str(v, be.thread()));
                        o.println(sb);
                    }
                } else if (e instanceof MethodExitEvent me) {
                    Method m = me.method();
                    if (m.isConstructor() || m.isStaticInitializer()) continue;
                    List<StackFrame> fs = me.thread().frames(0, Math.min(3, me.thread().frameCount()));
                    if (fs.size() > 1 && isRandom(fs.get(1).location().declaringType())) continue;
                    StringBuilder sb = new StringBuilder("X ").append(m.name()).append('(');
                    String caller = fs.size() > 1 ? " @" + fs.get(1).location().declaringType().name().replaceAll(".*\\.", "") + "." + fs.get(1).location().method().name() : "";
                    List<Value> avs = List.of();
                    try {
                        avs = fs.get(0).getArgumentValues();
                    } catch (Exception ignored) {}
                    for (Value v : avs) sb.append(str(v, me.thread())).append(',');
                    sb.append(") = ").append(str(me.returnValue(), me.thread()));
                    sb.append(caller);
                    o.println(sb);
                } else if (e instanceof VMDeathEvent || e instanceof VMDisconnectEvent) {
                    done = true;
                }
            }
            if (!done) es.resume();
        }
        o.flush();
        out.join();
        err.join();
        System.exit(proc.waitFor());
    }

    static final boolean THREADS = System.getenv("KREMENMC_JDI_THREADS") != null;
    static final boolean HEX = System.getenv("KREMENMC_JDI_HEX") != null;

    static boolean isRandom(ReferenceType t) {
        String n = t.name();
        return n.endsWith("RandomSource") || n.endsWith("WorldgenRandom") || n.endsWith("MarsagliaPolarGaussian") || n.endsWith("Xoroshiro128PlusPlus");
    }

    static String str(Value v, ThreadReference th) {
        if (v instanceof ObjectReference or && or.referenceType().name().endsWith("BlockState")) {
            try {
                Method ts = or.referenceType().methodsByName("toString", "()Ljava/lang/String;").get(0);
                return ((StringReference) or.invokeMethod(th, ts, List.of(), ObjectReference.INVOKE_SINGLE_THREADED)).value();
            } catch (Exception e) {
                return "BlockState?" + e;
            }
        }
        return str(v);
    }

    static String str(Value v) {
        if (v == null) return "null";
        if (v instanceof DoubleValue d) return HEX ? "d" + Long.toHexString(Double.doubleToRawLongBits(d.value())) : Double.toString(d.value());
        if (v instanceof FloatValue d) return HEX ? "f" + Integer.toHexString(Float.floatToRawIntBits(d.value())) : Float.toString(d.value()) + "f";
        if (v instanceof PrimitiveValue p) return p.toString();
        if (v instanceof StringReference s) return '"' + s.value() + '"';
        if (v instanceof ArrayReference ar) {
            StringBuilder sb = new StringBuilder("[");
            for (Value x : ar.getValues()) sb.append(str(x)).append(',');
            return sb.append(']').toString();
        }
        ObjectReference or = (ObjectReference) v;
        String t = or.referenceType().name().replaceAll(".*\\.", "");
        if (t.equals("ChunkPos") || t.endsWith("RandomSource") || t.equals("WorldgenRandom") || t.equals("WorldGenerationContext") || t.endsWith("BlockPos")) return t + "{" + fields(or, 0) + "}";
        return t.contains("$$Lambda") ? "lambda" : t;
    }

    static String fields(ObjectReference or, int depth) {
        StringBuilder sb = new StringBuilder();
        for (Field f : or.referenceType().allFields()) {
            if (f.isStatic()) continue;
            Value fv = or.getValue(f);
            if (fv instanceof PrimitiveValue) sb.append(f.name()).append('=').append(str(fv)).append(' ');
            else if (fv instanceof ObjectReference fo && depth < 3) {
                String ft = fo.referenceType().name();
                if (ft.endsWith("AtomicLong") || ft.endsWith("RandomSource") || ft.endsWith("WorldgenRandom"))
                    sb.append(f.name()).append("={").append(fields(fo, depth + 1)).append("} ");
            }
        }
        return sb.toString().trim();
    }

    static Thread pump(InputStream in, PrintStream to) {
        Thread t = new Thread(() -> {
            try { in.transferTo(to); } catch (IOException ignored) {}
        });
        t.start();
        return t;
    }
}
