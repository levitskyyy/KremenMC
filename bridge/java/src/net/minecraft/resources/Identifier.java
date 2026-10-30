package net.minecraft.resources;

import java.util.Objects;

public final class Identifier implements Comparable<Identifier> {
    public static final String DEFAULT_NAMESPACE = "minecraft";
    public static final char NAMESPACE_SEPARATOR = ':';

    private final String namespace;
    private final String path;

    private Identifier(String namespace, String path) {
        if (!valid(namespace, false) || !valid(path, true)) throw new IllegalArgumentException("invalid identifier " + namespace + ":" + path);
        this.namespace = namespace;
        this.path = path;
    }

    public static Identifier fromNamespaceAndPath(String namespace, String path) { return new Identifier(namespace, path); }

    public static Identifier withDefaultNamespace(String path) { return new Identifier(DEFAULT_NAMESPACE, path); }

    public static Identifier parse(String s) {
        int i = s.indexOf(NAMESPACE_SEPARATOR);
        if (i < 0) return withDefaultNamespace(s);
        String ns = s.substring(0, i);
        return new Identifier(ns.isEmpty() ? DEFAULT_NAMESPACE : ns, s.substring(i + 1));
    }

    public static Identifier tryParse(String s) {
        try {
            return parse(s);
        } catch (IllegalArgumentException e) {
            return null;
        }
    }

    public static Identifier bySeparator(String s, char sep) {
        int i = s.indexOf(sep);
        return i < 0 ? withDefaultNamespace(s) : new Identifier(s.substring(0, i), s.substring(i + 1));
    }

    private static boolean valid(String s, boolean path) {
        for (int i = 0; i < s.length(); i++) {
            char c = s.charAt(i);
            boolean ok = c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-' || c == '.' || path && c == '/';
            if (!ok) return false;
        }
        return true;
    }

    public String getNamespace() { return namespace; }
    public String getPath() { return path; }

    public Identifier withPath(String p) { return new Identifier(namespace, p); }

    @Override public String toString() { return namespace + NAMESPACE_SEPARATOR + path; }
    @Override public boolean equals(Object o) { return o instanceof Identifier r && r.namespace.equals(namespace) && r.path.equals(path); }
    @Override public int hashCode() { return Objects.hash(namespace, path); }

    @Override
    public int compareTo(Identifier o) {
        int c = path.compareTo(o.path);
        return c != 0 ? c : namespace.compareTo(o.namespace);
    }
}
