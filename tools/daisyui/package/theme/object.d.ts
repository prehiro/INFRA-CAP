interface Theme {
  "color-scheme": string
  "--color-base-100": string
  "--color-base-200": string
  "--color-base-300": string
  "--color-base-content": string
  "--color-primary": string
  "--color-primary-content": string
  "--color-secondary": string
  "--color-secondary-content": string
  "--color-accent": string
  "--color-accent-content": string
  "--color-neutral": string
  "--color-neutral-content": string
  "--color-info": string
  "--color-info-content": string
  "--color-success": string
  "--color-success-content": string
  "--color-warning": string
  "--color-warning-content": string
  "--color-error": string
  "--color-error-content": string
  "--radius-selector": string
  "--radius-field": string
  "--radius-box": string
  "--size-selector": string
  "--size-field": string
  "--border": string
  "--depth": string
  "--noise": string
}


interface Themes {
  night: Theme
  winter: Theme
  garden: Theme
  lofi: Theme
  acid: Theme
  caramellatte: Theme
  cupcake: Theme
  black: Theme
  abyss: Theme
  nord: Theme
  silk: Theme
  corporate: Theme
  fantasy: Theme
  halloween: Theme
  wireframe: Theme
  sunset: Theme
  synthwave: Theme
  dim: Theme
  retro: Theme
  pastel: Theme
  forest: Theme
  luxury: Theme
  light: Theme
  cyberpunk: Theme
  bumblebee: Theme
  business: Theme
  aqua: Theme
  cmyk: Theme
  lemonade: Theme
  autumn: Theme
  valentine: Theme
  dark: Theme
  coffee: Theme
  dracula: Theme
  emerald: Theme
  [key: string]: Theme
}

declare const themes: Themes
export default themes