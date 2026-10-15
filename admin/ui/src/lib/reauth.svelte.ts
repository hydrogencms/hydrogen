class Reauth {
  /** True while the "session expired" dialog is up (a 401 arrived while a form had unsaved changes). */
  open = $state(false);
}

export const reauth = new Reauth();
