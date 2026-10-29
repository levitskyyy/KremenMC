package kremenmc.probe.jigsaw;

import com.sun.jdi.*;
import com.sun.jdi.connect.*;
import com.sun.jdi.event.*;
import com.sun.jdi.request.*;
import java.io.*;
import java.util.*;

// JigJdi runs JigStarts in a debuggee JVM and, for every entry into the
// public Shapes methods joinIsNotEmpty and join while JigStarts.draws is in
// [lo,hi], prints the draw index, the method and its argument values, with
// VoxelShapes expanded through their public toAabbs() and BooleanOps named
// by the BooleanOp constant they equal. Observation only.
// Args: <lo> <hi> <JigStarts args...>
public final class JigJdi {
    public static void main(String[] a) throws Exception {
        int lo = Integer.parseInt(a[0]), hi = Integer.parseInt(a[1]);
        LaunchingConnector lc = Bootstrap.virtualMachineManager().defaultConnector();
        Map<String, Connector.Argument> args = lc.defaultArguments();
        args.get("main").setValue("kremenmc.probe.jigsaw.JigStarts " + String.join(" ", Arrays.copyOfRange(a, 2, a.length)));
        args.get("options").setValue("-Xmx2g -Dkremenmc.jig.lo=" + lo + " -Dkremenmc.jig.hi=" + hi + " -cp " + System.getProperty("java.class.path"));
        VirtualMachine vm = lc.launch(args);
        Process proc = vm.process();
        Thread err = pump(proc.getErrorStream(), new PrintStream(OutputStream.nullOutputStream()));
        Thread out = pump(proc.getInputStream(), System.out);
        PrintStream o = System.out;
        EventRequestManager erm = vm.eventRequestManager();
        ClassPrepareRequest cp = erm.createClassPrepareRequest();
        cp.addClassFilter("net.minecraft.world.phys.shapes.Shapes");
        cp.setSuspendPolicy(EventRequest.SUSPEND_ALL);
        cp.enable();
        ClassPrepareRequest cp2 = erm.createClassPrepareRequest();
        cp2.addClassFilter("kremenmc.probe.jigsaw.JigStarts");
        cp2.setSuspendPolicy(EventRequest.SUSPEND_ALL);
        cp2.enable();
        List<BreakpointRequest> shapeBps = new ArrayList<>();
        EventQueue q = vm.eventQueue();
        boolean done = false;
        while (!done) {
            EventSet es = q.remove();
            for (Event e : es) {
                if (e instanceof ClassPrepareEvent cpe) {
                    for (Method m : cpe.referenceType().methods()) {
                        boolean mk = m.name().equals("mark") && cpe.referenceType().name().endsWith("JigStarts");
                        if ((mk || m.name().equals("joinIsNotEmpty") || m.name().equals("join")) && m.location() != null) {
                            BreakpointRequest br = erm.createBreakpointRequest(m.location());
                            br.setSuspendPolicy(EventRequest.SUSPEND_EVENT_THREAD);
                            if (mk) br.enable();
                            else shapeBps.add(br);
                        }
                    }
                } else if (e instanceof BreakpointEvent be && be.location().method().name().equals("mark")) {
                    boolean on = ((BooleanValue) be.thread().frame(0).getArgumentValues().get(0)).value();
                    shapeBps.forEach(b -> b.setEnabled(on));
                } else if (e instanceof BreakpointEvent be) {
                    var js = vm.classesByName("kremenmc.probe.jigsaw.JigStarts");
                    if (js.isEmpty()) continue;
                    int d = ((IntegerValue) js.get(0).getValue(js.get(0).fieldByName("draws"))).value();
                    if (d < lo || d > hi) continue;
                    ThreadReference th = be.thread();
                    StackFrame f = th.frame(0);
                    StringBuilder sb = new StringBuilder("d=" + d + " " + be.location().method().name());
                    String caller = th.frameCount() > 1 ? th.frame(1).location().declaringType().name().replaceAll(".*\\.", "") + "." + th.frame(1).location().method().name() + ":" + th.frame(1).location().lineNumber() : "";
                    sb.append(" @").append(caller);
                    List<Value> vs = new ArrayList<>(f.getArgumentValues());
                    for (Value v : vs) sb.append(" | ").append(describe(v, th, vm));
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
    }

    static String describe(Value v, ThreadReference th, VirtualMachine vm) {
        try {
            if (!(v instanceof ObjectReference or)) return String.valueOf(v);
            String t = or.referenceType().name();
            var bo = vm.classesByName("net.minecraft.world.phys.shapes.BooleanOp");
            if (!bo.isEmpty()) for (Field fl : bo.get(0).allFields()) if (fl.isStatic() && or.equals(bo.get(0).getValue(fl))) return "op:" + fl.name();
            Method ta = null;
            for (Method m : or.referenceType().methodsByName("toAabbs")) ta = m;
            if (ta != null) {
                ObjectReference list = (ObjectReference) or.invokeMethod(th, ta, List.of(), ObjectReference.INVOKE_SINGLE_THREADED);
                Method size = list.referenceType().methodsByName("size").get(0);
                Method get = list.referenceType().methodsByName("get").get(0);
                int n = ((IntegerValue) list.invokeMethod(th, size, List.of(), ObjectReference.INVOKE_SINGLE_THREADED)).value();
                StringBuilder sb = new StringBuilder("shape[" + n + "]");
                if (BRIEF && n > 4) return sb.toString();
                for (int i = 0; i < n && i < 400; i++) {
                    ObjectReference ab = (ObjectReference) list.invokeMethod(th, get, List.of(vm.mirrorOf(i)), ObjectReference.INVOKE_SINGLE_THREADED);
                    sb.append(" (");
                    for (String fn : new String[]{"minX", "minY", "minZ", "maxX", "maxY", "maxZ"}) sb.append(((DoubleValue) ab.getValue(ab.referenceType().fieldByName(fn))).value()).append(',');
                    sb.append(')');
                }
                return sb.toString();
            }
            return t;
        } catch (Exception ex) {
            return "?" + ex;
        }
    }

    static final boolean BRIEF = System.getenv("KREMENMC_JIG_BRIEF") != null;

    static Thread pump(InputStream in, PrintStream to) {
        Thread t = new Thread(() -> {
            try { in.transferTo(to); } catch (IOException ignored) {}
        });
        t.start();
        return t;
    }
}
