package kremenmc.bridge;

import static java.lang.classfile.ClassFile.ACC_FINAL;
import static java.lang.classfile.ClassFile.ACC_PRIVATE;
import static java.lang.classfile.ClassFile.ACC_PUBLIC;
import static java.lang.classfile.ClassFile.ACC_STATIC;
import static java.lang.classfile.ClassFile.ACC_SYNTHETIC;
import static java.lang.constant.ConstantDescs.CD_Object;
import static java.lang.constant.ConstantDescs.CD_void;
import static java.lang.constant.ConstantDescs.INIT_NAME;

import java.lang.classfile.ClassFile;
import java.lang.classfile.CodeBuilder;
import java.lang.classfile.TypeKind;
import java.lang.constant.ClassDesc;
import java.lang.constant.MethodTypeDesc;
import java.lang.invoke.MethodHandles;
import java.lang.reflect.Constructor;
import java.lang.reflect.InvocationHandler;
import java.lang.reflect.InvocationTargetException;
import java.lang.reflect.Method;
import java.lang.reflect.Modifier;
import java.util.ArrayDeque;
import java.util.ArrayList;
import java.util.Deque;
import java.util.HashSet;
import java.util.LinkedHashMap;
import java.util.List;
import java.util.Map;
import java.util.Set;
import java.util.concurrent.ConcurrentHashMap;

/**
 * Like java.lang.reflect.Proxy, but the generated class extends an abstract
 * base class, so plugins that cast to a server class (CraftPlayer) work. The
 * base either has a constructor taking an InvocationHandler stored in a
 * protected field named "handler", or a no-argument constructor (an API
 * class such as PotionEffectType). Every method left abstract, every
 * interface default method not overridden by the base, plus equals, hashCode
 * and toString, calls the handler. A default method also gets a
 * "$super$name" twin that runs the interface's own body (see invokeSuper).
 */
final class ClassProxy {
    private ClassProxy() {}

    private static final Map<Class<?>, Constructor<?>> ctors = new ConcurrentHashMap<>();
    private static final ClassDesc CD_HANDLER = desc(InvocationHandler.class);
    private static final ClassDesc CD_METHOD = desc(Method.class);
    private static final MethodTypeDesc MT_INIT = MethodTypeDesc.of(CD_void, CD_HANDLER);
    private static final MethodTypeDesc MT_INVOKE = MethodTypeDesc.of(CD_Object, CD_Object, CD_METHOD, CD_Object.arrayType());

    static void prepare(Class<?> base, Class<?>... extra) {
        ctors.computeIfAbsent(base, k -> define(k, extra));
    }

    static Object create(Class<?> base, InvocationHandler h, Class<?>... extra) {
        Constructor<?> c = ctors.computeIfAbsent(base, k -> define(k, extra));
        try {
            return c.newInstance(h);
        } catch (InvocationTargetException e) {
            throw new IllegalStateException(e.getCause());
        } catch (ReflectiveOperationException e) {
            throw new IllegalStateException(e);
        }
    }

    private static Constructor<?> define(Class<?> base, Class<?>[] extra) {
        List<Method> methods = abstractMethods(base, extra);
        try {
            for (String n : List.of("equals", "hashCode", "toString")) {
                methods.add(n.equals("equals") ? Object.class.getMethod(n, Object.class) : Object.class.getMethod(n));
            }
            ClassDesc self = ClassDesc.of(implName(base));
            ClassDesc parent = desc(base);
            boolean own = !hasHandlerConstructor(base);
            ClassDesc holder = own ? self : parent;
            byte[] bytes = ClassFile.of().build(self, cb -> {
                cb.withFlags(ACC_PUBLIC | ACC_FINAL | ACC_SYNTHETIC);
                cb.withSuperclass(parent);
                Set<Class<?>> ifaces = new java.util.LinkedHashSet<>(List.of(extra));
                for (Method m : methods) if (m.isDefault()) ifaces.add(m.getDeclaringClass());
                cb.withInterfaceSymbols(ifaces.stream().map(ClassProxy::desc).toList());
                cb.withField("M", CD_METHOD.arrayType(), ACC_PUBLIC | ACC_STATIC);
                if (own) {
                    cb.withField("handler", CD_HANDLER, ACC_PRIVATE | ACC_FINAL);
                    cb.withMethodBody(INIT_NAME, MT_INIT, ACC_PUBLIC, code -> code
                            .aload(0).invokespecial(parent, INIT_NAME, MethodTypeDesc.of(CD_void))
                            .aload(0).aload(1).putfield(self, "handler", CD_HANDLER).return_());
                } else {
                    cb.withMethodBody(INIT_NAME, MT_INIT, ACC_PUBLIC, code -> code
                            .aload(0).aload(1).invokespecial(parent, INIT_NAME, MT_INIT).return_());
                }
                for (int i = 0; i < methods.size(); i++) {
                    Method m = methods.get(i);
                    int idx = i;
                    cb.withMethodBody(m.getName(), type(m), ACC_PUBLIC, code -> body(code, self, holder, m, idx));
                    if (m.isDefault()) cb.withMethodBody(SUPER + m.getName(), type(m), ACC_PUBLIC | ACC_SYNTHETIC, code -> superBody(code, m));
                }
            });
            Class<?> cls = MethodHandles.privateLookupIn(base, MethodHandles.lookup()).defineClass(bytes);
            cls.getField("M").set(null, methods.toArray(new Method[0]));
            return cls.getConstructor(InvocationHandler.class);
        } catch (ReflectiveOperationException e) {
            throw new IllegalStateException("generate subclass of " + base.getName(), e);
        }
    }

    private static boolean hasHandlerConstructor(Class<?> base) {
        try {
            base.getDeclaredConstructor(InvocationHandler.class);
            return true;
        } catch (NoSuchMethodException e) {
            return false;
        }
    }

    /** AbstractFoo gets the concrete subclass Foo in the same package, anything else Base$Impl. */
    static String implName(Class<?> base) {
        String n = base.getSimpleName();
        if (n.startsWith("Abstract") && n.length() > 8) return base.getPackageName() + "." + n.substring(8);
        return base.getName() + "$Impl";
    }

    private static void body(CodeBuilder code, ClassDesc self, ClassDesc holder, Method m, int idx) {
        code.aload(0).getfield(holder, "handler", CD_HANDLER);
        code.aload(0);
        code.getstatic(self, "M", CD_METHOD.arrayType()).loadConstant(idx).aaload();
        Class<?>[] ps = m.getParameterTypes();
        if (ps.length == 0) {
            code.aconst_null();
        } else {
            code.loadConstant(ps.length).anewarray(CD_Object);
            int slot = 1;
            for (int j = 0; j < ps.length; j++) {
                TypeKind k = TypeKind.from(desc(ps[j]));
                code.dup().loadConstant(j).loadLocal(k, slot);
                if (ps[j].isPrimitive()) box(code, ps[j]);
                code.aastore();
                slot += k.slotSize();
            }
        }
        code.invokeinterface(CD_HANDLER, "invoke", MT_INVOKE);
        Class<?> r = m.getReturnType();
        if (r == void.class) {
            code.pop().return_();
        } else if (r.isPrimitive()) {
            ClassDesc w = desc(wrapper(r));
            code.checkcast(w).invokevirtual(w, r.getName() + "Value", MethodTypeDesc.of(desc(r)));
            code.return_(TypeKind.from(desc(r)));
        } else {
            code.checkcast(desc(r)).areturn();
        }
    }

    private static final String SUPER = "$super$";
    private static final Map<Method, Method> supers = new ConcurrentHashMap<>();

    /** Runs the interface body of default method m on a generated instance. */
    static Object invokeSuper(Object proxy, Method m, Object[] args) throws Throwable {
        Method t = supers.computeIfAbsent(m, k -> {
            try {
                return proxy.getClass().getMethod(SUPER + k.getName(), k.getParameterTypes());
            } catch (NoSuchMethodException e) {
                return null;
            }
        });
        if (t == null) return Stubs.fallback(m.getDeclaringClass(), m);
        try {
            return t.invoke(proxy, args);
        } catch (InvocationTargetException e) {
            throw e.getCause();
        }
    }

    private static void superBody(CodeBuilder code, Method m) {
        code.aload(0);
        int slot = 1;
        for (Class<?> p : m.getParameterTypes()) {
            TypeKind k = TypeKind.from(desc(p));
            code.loadLocal(k, slot);
            slot += k.slotSize();
        }
        code.invokespecial(desc(m.getDeclaringClass()), m.getName(), type(m), true);
        Class<?> r = m.getReturnType();
        if (r == void.class) code.return_();
        else code.return_(TypeKind.from(desc(r)));
    }

    private static boolean overridable(Method m) {
        if (!Modifier.isPublic(m.getDeclaringClass().getModifiers())) return false;
        for (Class<?> p : m.getParameterTypes()) if (!visible(p)) return false;
        return visible(m.getReturnType());
    }

    private static boolean visible(Class<?> c) {
        while (c.isArray()) c = c.getComponentType();
        return c.isPrimitive() || Modifier.isPublic(c.getModifiers());
    }

    private static void box(CodeBuilder code, Class<?> p) {
        ClassDesc w = desc(wrapper(p));
        code.invokestatic(w, "valueOf", MethodTypeDesc.of(w, desc(p)));
    }

    private static Class<?> wrapper(Class<?> p) {
        return switch (p.getName()) {
            case "boolean" -> Boolean.class;
            case "byte" -> Byte.class;
            case "char" -> Character.class;
            case "short" -> Short.class;
            case "int" -> Integer.class;
            case "long" -> Long.class;
            case "float" -> Float.class;
            default -> Double.class;
        };
    }

    private static ClassDesc desc(Class<?> c) { return c.describeConstable().orElseThrow(); }

    private static MethodTypeDesc type(Method m) {
        Class<?>[] ps = m.getParameterTypes();
        ClassDesc[] d = new ClassDesc[ps.length];
        for (int i = 0; i < ps.length; i++) d[i] = desc(ps[i]);
        return MethodTypeDesc.of(desc(m.getReturnType()), d);
    }

    /** One method per name and descriptor not implemented by a class: abstract, or a single most specific default. */
    static List<Method> abstractMethods(Class<?> base, Class<?>... extra) {
        Map<String, List<Method>> groups = new LinkedHashMap<>();
        Set<Class<?>> seen = new HashSet<>();
        Deque<Class<?>> todo = new ArrayDeque<>();
        todo.add(base);
        todo.addAll(List.of(extra));
        while (!todo.isEmpty()) {
            Class<?> c = todo.poll();
            if (!seen.add(c)) continue;
            for (Method m : c.getDeclaredMethods()) {
                int mod = m.getModifiers();
                if (Modifier.isStatic(mod) || Modifier.isPrivate(mod)) continue;
                groups.computeIfAbsent(m.getName() + type(m).descriptorString(), k -> new ArrayList<>()).add(m);
            }
            if (c.getSuperclass() != null) todo.add(c.getSuperclass());
            todo.addAll(List.of(c.getInterfaces()));
        }
        List<Method> out = new ArrayList<>();
        for (List<Method> g : groups.values()) {
            if (g.stream().anyMatch(m -> !m.getDeclaringClass().isInterface() && !Modifier.isAbstract(m.getModifiers()))) continue;
            List<Method> top = new ArrayList<>();
            for (Method m : g) {
                boolean shadowed = false;
                for (Method o : g) {
                    if (o != m && o.getDeclaringClass() != m.getDeclaringClass() && m.getDeclaringClass().isAssignableFrom(o.getDeclaringClass())) {
                        shadowed = true;
                        break;
                    }
                }
                if (!shadowed) top.add(m);
            }
            Method rep = top.stream().filter(m -> Modifier.isAbstract(m.getModifiers())).findFirst().orElse(top.getFirst());
            if (rep.isDefault() && (top.size() > 1 || !overridable(rep))) continue;
            out.add(rep);
        }
        return out;
    }
}
